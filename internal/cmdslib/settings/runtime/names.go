package runtime

import (
	"fmt"
	"path"
	"runtime"
	"strings"
	"unicode"

	"github.com/fatih/camelcase"
)

type stringMapperFunc func(r rune) rune

func filter(components []string, filters ...stringMapperFunc) []string {
	for _, filter := range filters {
		filtered := make([]string, 0, len(components))
		for _, component := range components {
			if component := strings.Map(filter, component); component != "" {
				filtered = append(filtered, component)
			}
		}
		components = filtered
	}
	return components
}

func filterCli(parameterRune rune) rune {
	if unicode.IsSpace(parameterRune) ||
		parameterRune == '=' {
		return -1
	}
	return parameterRune
}

func filterEnv(keyRune rune) rune {
	if unicode.IsSpace(keyRune) ||
		keyRune == '=' {
		return -1
	}
	if keyRune == '.' {
		return '_'
	}
	return keyRune
}

func filterRuntime(refRune rune) rune {
	// NOTE: references from methods usually look like: `pkg.(*Type).Method`.
	if refRune == '(' ||
		refRune == '*' ||
		refRune == ')' {
		return -1
	}
	return refRune
}

func cliName(name string) string {
	var (
		splitName = camelcase.Split(name)
		cleaned   = filter(splitName, filterCli)
		clName    = strings.ToLower(strings.Join(cleaned, "-"))
	)
	return clName
}

func envName(prefix, namespace, name string) string {
	var (
		components []string
		splitName  = camelcase.Split(name)
	)
	if prefix != "" {
		splitPrefix := strings.Split(prefix, " ")
		components = append(components, splitPrefix...)
	}
	if namespace != "" {
		splitNamespace := strings.Split(namespace, " ")
		components = append(components, splitNamespace...)
	}
	components = append(components, splitName...)
	var (
		cleaned = filter(components, filterEnv)
		envName = strings.ToUpper(strings.Join(cleaned, "_"))
	)
	return envName
}

func funcNames(instructionPointer uintptr) (namespace, name string) {
	var (
		// Documentation refers to this as a
		// "package path-qualified function name".
		// Typically looks like: `pkgName.referenceName`,
		// `pkgName.referenceName.deeperReference-name`, etc.
		ppqfn = runtime.FuncForPC(instructionPointer).Name()
		names = strings.Split(path.Base(ppqfn), ".")
	)
	namesEnd := len(names)
	if namesEnd < 2 {
		panic(fmt.Sprintf(
			"runtime returned non-standard function name"+
				"\n\tgot: `%s`"+
				"\n\twant format: `$pkgName.$funcName`",
			ppqfn,
		))
	}
	filteredNames := filter([]string{
		names[0],
		names[namesEnd-1],
	}, filterRuntime)

	namespace = filteredNames[0]
	name = filteredNames[1]
	return
}
