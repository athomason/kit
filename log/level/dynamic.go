package level

import (
	"fmt"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-kit/kit/log"
)

type DynamicFilter interface {
	log.Logger

	// Override specifies a program location under which Log calls will be
	// specially treated with respect to log levels. Log behaves as with the
	// regular leveled logger, except when an override has been set for some
	// location in the call stack. The nearest override up the stack takes
	// precedence. If duration is non-zero, the override will be removed
	// afterwards. Override is safe for concurrent use.
	Override(file string, line int, b behavior, duration time.Duration) error

	// Overrides returns a list of current overrides.
	Overrides() []Override
}

type behavior int

const (
	_ behavior = iota

	LogAlways  // Log calls under the target site are always enabled
	LogNever   // Log calls under the target site are never enabled
	LogLeveled // Log calls follow normal level rules
)

type dynamic struct {
	next   log.Logger // original logger
	filter *logger    // NewFilter wrapper

	mu        sync.Mutex   // guards overrides.Store
	overrides atomic.Value // map[callsite]behavior
}

// NewDynamicFilter wraps NewFilter. The returned DynamicFilter, which is a
// Logger, may be updated with calls to Override.
func NewDynamicFilter(next log.Logger, options ...Option) DynamicFilter {
	d := &dynamic{
		next:   next,
		filter: NewFilter(next, options...).(*logger),
	}
	d.overrides.Store(map[callsite]behavior{})
	return d
}

func (d *dynamic) Override(file string, line int, b behavior, dur time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// copy the list of overrides
	olds := d.overrides.Load().(map[callsite]behavior)
	news := make(map[callsite]behavior, len(olds))
	for k, v := range olds {
		news[k] = v
	}

	// make the requested adjustment
	key := mapKey(file, line)
	switch b {
	case LogAlways, LogNever:
		news[key] = b
	case LogLeveled:
		delete(news, key)
	default:
		return fmt.Errorf("invalid behavior %v", b)
	}

	d.overrides.Store(news)

	if dur > 0 {
		go func() {
			time.Sleep(dur)
			d.Override(file, line, LogLeveled, 0)
		}()
	}

	return nil
}

type Override struct {
	File       string
	Line       int
	LogEnabled bool
}

type overrides []Override

func (o overrides) Len() int      { return len(o) }
func (o overrides) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o overrides) Less(i, j int) bool {
	if o[i].File < o[j].File {
		return true
	} else if o[j].File < o[i].File {
		return false
	}
	return o[i].Line < o[j].Line
}

func (d *dynamic) Overrides() []Override {
	cur := d.overrides.Load().(map[callsite]behavior)
	o := make([]Override, 0, len(cur))
	for k, v := range cur {
		o = append(o, Override{
			File:       k.file,
			Line:       k.line,
			LogEnabled: v == LogAlways,
		})
	}
	sort.Sort(overrides(o))
	return o
}

func (d *dynamic) Log(keyvals ...interface{}) error {
	overrides := d.overrides.Load().(map[callsite]behavior)
	if len(overrides) == 0 {
		return d.filter.Log(keyvals...)
	}

	// get full stack trace
	callers := make([]uintptr, 16)
	for {
		n := runtime.Callers(3, callers)
		if n < len(callers) {
			callers = callers[:n]
			break
		}
		callers = make([]uintptr, 2*len(callers))
	}
	frames := runtime.CallersFrames(callers)

	// look for the nearest (deepest) override
	for i := 0; ; i++ {
		f, more := frames.Next()
		if b, ok := overrides[mapKey(f.File, f.Line)]; ok {
			if b == LogAlways {
				return d.next.Log(keyvals...) // skip level filtering
			} else {
				return d.filter.errNotAllowed
			}
		}
		if !more {
			break
		}
	}

	// no overrides found, forward to normal filterer
	return d.filter.Log(keyvals...)
}

type callsite struct {
	file string
	line int
}

func mapKey(file string, line int) callsite { return callsite{file: file, line: line} }
