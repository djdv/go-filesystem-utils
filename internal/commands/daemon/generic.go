package daemon

import (
	"reflect"
	"sync"
)

type (
	waitGroupChan[T any] struct {
		ch      chan T
		closing chan struct{}
		sync.WaitGroup
	}
	wgErrs     = *waitGroupChan[error]
	wgShutdown = *waitGroupChan[ShutdownDisposition]
)

func newWaitGroupChan[T any](size int) *waitGroupChan[T] {
	return &waitGroupChan[T]{
		ch:      make(chan T),
		closing: make(chan struct{}, size),
	}
}

func (wc *waitGroupChan[T]) Closing() <-chan struct{} {
	return wc.closing
}

func (wc *waitGroupChan[T]) closeSend() {
	close(wc.closing)
}

func (wc *waitGroupChan[T]) send(value T) (sent bool) {
	select {
	case wc.ch <- value:
		sent = true
	case <-wc.closing:
	}
	return sent
}

func (wc *waitGroupChan[T]) waitThenCloseCh() {
	wc.WaitGroup.Wait()
	close(wc.ch)
}

func relayUnordered[T any](in <-chan T, outs ...*<-chan T) {
	chs := make([]chan<- T, len(outs))
	for i := range outs {
		ch := make(chan T, cap(in))
		*outs[i] = ch
		chs[i] = ch
	}
	go relayChan(in, chs...)
}

// relayChan will relay values (in a non-blocking manner)
// from `source` to all `relays` (immediately or eventually).
// The source must be closed to stop processing.
// Each relay is closed after all values are sent.
// Relay receive order is not guaranteed to match
// source's order.
func relayChan[T any](source <-chan T, relays ...chan<- T) {
	var (
		relayValues  = reflectSendChans(relays...)
		relayCount   = len(relayValues)
		disabledCase = reflect.Value{}
		defaultCase  = relayCount
		cases        = make([]reflect.SelectCase, defaultCase+1)
		closerWgs    = make([]*sync.WaitGroup, relayCount)
		send         = func(wg *sync.WaitGroup, ch chan<- T, value T) {
			ch <- value
			wg.Done()
		}
	)
	cases[defaultCase] = reflect.SelectCase{Dir: reflect.SelectDefault}
	for value := range source {
		populateSelectSendCases(value, relayValues, cases)
		for remaining := relayCount; remaining != 0; {
			chosen, _, _ := reflect.Select(cases)
			if chosen != defaultCase {
				cases[chosen].Chan = disabledCase
				remaining--
				continue
			}
			for i, commCase := range cases[:relayCount] {
				if !commCase.Chan.IsValid() {
					continue // Already sent.
				}
				wg := closerWgs[i]
				if wg == nil {
					wg = new(sync.WaitGroup)
					closerWgs[i] = wg
				}
				wg.Add(1)
				go send(wg, relays[i], value)
			}
			break
		}
	}
	waitAndClose := func(wg *sync.WaitGroup, ch chan<- T) {
		wg.Wait()
		close(ch)
	}
	for i, wg := range closerWgs {
		if wg == nil {
			close(relays[i])
			continue
		}
		go waitAndClose(wg, relays[i])
	}
}

func reflectSendChans[T any](chans ...chan<- T) []reflect.Value {
	values := make([]reflect.Value, len(chans))
	for i, relay := range chans {
		values[i] = reflect.ValueOf(relay)
	}
	return values
}

// populateSelectSendCases will create
// send cases containing `value` for
// each channel in `channels`, and assign it
// within `cases`. Panics if len(cases) < len(channels).
func populateSelectSendCases[T any](value T, channels []reflect.Value, cases []reflect.SelectCase) {
	rValue := reflect.ValueOf(value)
	for i, channel := range channels {
		cases[i] = reflect.SelectCase{
			Dir:  reflect.SelectSend,
			Chan: channel,
			Send: rValue,
		}
	}
}
