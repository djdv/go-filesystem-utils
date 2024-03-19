package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// NOTE: On signal handling;
// Go's default signal handler translates some messages
// into [syscall.SIGTERM] (see [os/signal] documentation).
// Windows has at least 3 forms of messaging that apply to us.
// 1) Control signals.
// 2) Window messages.
// 3) Service events.
// 1 applies to [syscall.SIGTERM], with exceptions.
// `HandlerRoutine` will not receive `CTRL_LOGOFF_EVENT` nor
// `CTRL_SHUTDOWN_EVENT` for "interactive applications"
// (applications which link with `user32`;
// see `SetConsoleCtrlHandler` documentation).
//
// We utilize `user32` (indirectly through the [xdg] package)
// which flags us as interactive. As such, we need to
// initialize a window message queue (2) and monitor it for
// these events. The console handler (1) can (and must) be
// registered simultaneously, to handle the other signals
// such as interrupt, break, close, etc.
//
// We do not yet handle case 3.

type wndProcFunc func(win.HWND, uint32, uintptr, uintptr) uintptr

func registerSystemStoppers(ctx context.Context, shutdownSend wgShutdown) {
	shutdownSend.Add(2)
	// NOTE: [Go 1.20] This must be `syscall.SIGTERM`
	// not `windows.SIGTERM`, otherwise the runtime
	// will not set up the console control handler.
	go stopOnSignalLinear(shutdownSend, syscall.SIGTERM)
	go stopOnSignalLinear(shutdownSend, os.Interrupt)
	if err := createAndWatchWindow(ctx, shutdownSend); err != nil {
		panic(err)
	}
}

func stopOnSignalLinear(shutdownSend wgShutdown, sig os.Signal) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, sig)
	defer func() {
		signal.Stop(signals)
		shutdownSend.Done()
	}()
	for count := minimumShutdown; count <= maximumShutdown; count++ {
		select {
		case <-signals:
			if !shutdownSend.send(count) {
				return
			}
		case <-shutdownSend.Closing():
			return
		}
	}
}

func createAndWatchWindow(ctx context.Context, shutdownSend wgShutdown) error {
	errs := make(chan error, 1)
	shutdownSend.Add(1)
	go func() {
		runtime.LockOSThread() // The window and message processor
		defer func() {         // must be on the same thread.
			runtime.UnlockOSThread()
			shutdownSend.Done()
		}()
		hWnd, err := createEventWindow("go-fs", shutdownSend)
		errs <- err
		if err != nil {
			return
		}
		closeWindowWhenDone := func() {
			select {
			case <-ctx.Done():
			case <-shutdownSend.Closing():
			}
			const (
				NULL   = 0
				wParam = NULL
				lParam = NULL
			)
			win.SendMessage(hWnd, win.WM_CLOSE, wParam, lParam)
		}
		go closeWindowWhenDone()
		const (
			// Ignore C's `BOOL` declaration for `GetMessage`
			// it actually returns a trinary value. See MSDN docs.
			failed       = -1
			wmQuit       = 0
			success      = 1
			NULL         = 0
			msgFilterMin = NULL
			msgFilterMax = NULL
		)
		for {
			var msg win.MSG
			switch win.GetMessage(&msg, hWnd, msgFilterMin, msgFilterMax) {
			case failed, wmQuit:
				// NOTE: If we fail here the error
				// (`GetLastError`) is dropped.
				// Given our parameter set, failure
				// implies the window handle was (somehow)
				// invalidated, so we can't continue.
				// This is very unlikely to happen on accident.
				// Especially outside of development.
				return
			case success:
				win.TranslateMessage(&msg)
				win.DispatchMessage(&msg)
			}
		}
	}()
	return <-errs
}

func createEventWindow(name string, shutdownSend wgShutdown) (win.HWND, error) {
	const INVALID_HANDLE_VALUE win.HWND = ^win.HWND(0)
	lpClassName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return INVALID_HANDLE_VALUE, err
	}
	var (
		hInstance   = win.GetModuleHandle(nil)
		windowClass = win.WNDCLASSEX{
			LpfnWndProc:   newWndProc(shutdownSend),
			HInstance:     hInstance,
			LpszClassName: lpClassName,
		}
	)
	windowClass.CbSize = uint32(unsafe.Sizeof(windowClass))
	_ = win.RegisterClassEx(&windowClass)
	const (
		NULL       = 0
		dwExStyle  = NULL
		dwStyle    = NULL
		x          = NULL
		y          = NULL
		nWidth     = NULL
		nHeight    = NULL
		hWndParent = NULL
		hMenu      = NULL
	)
	var (
		lpWindowName *uint16        = nil
		lpParam      unsafe.Pointer = nil
		hWnd                        = win.CreateWindowEx(
			dwExStyle,
			lpClassName, lpWindowName,
			dwStyle,
			x, y,
			nWidth, nHeight,
			hWndParent, hMenu,
			hInstance, lpParam,
		)
	)
	if hWnd == NULL {
		var err error = generic.ConstError(
			"CreateWindowEx failed",
		)
		if lErr := windows.GetLastError(); lErr != nil {
			err = fmt.Errorf("%w: %w", err, lErr)
		}
		return INVALID_HANDLE_VALUE, err
	}
	return hWnd, nil
}

func newWndProc(shutdownSend wgShutdown) uintptr {
	return windows.NewCallback(
		func(hWnd win.HWND, uMsg uint32, wParam uintptr, lParam uintptr) uintptr {
			switch uMsg {
			case win.WM_QUERYENDSESSION:
				const shutdownOrRestart = 0
				var disposition ShutdownDisposition
				switch {
				case lParam == shutdownOrRestart:
					disposition = ShutdownImmediate
				case lParam&win.ENDSESSION_LOGOFF != 0:
					disposition = ShutdownShort
				case lParam&win.ENDSESSION_CRITICAL != 0:
					disposition = ShutdownImmediate
				default:
					disposition = ShutdownImmediate
				}
				shutdownSend.send(disposition)
				const (
					FALSE    = 0
					canClose = FALSE
				)
				return canClose
			case win.WM_CLOSE:
				const processedToken = 0
				shutdownSend.send(ShutdownImmediate)
				win.DestroyWindow(hWnd)
				return processedToken
			case win.WM_DESTROY:
				const processedToken = 0
				shutdownSend.send(ShutdownImmediate)
				const toCallingThread = 0
				win.PostQuitMessage(toCallingThread)
				return processedToken
			default:
				return win.DefWindowProc(hWnd, uMsg, wParam, lParam)
			}
		})
}
