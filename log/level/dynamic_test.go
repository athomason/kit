package level_test

import (
	"errors"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

func TestDynamicFilter(t *testing.T) {
	myError := errors.New("squelched!")
	opts := []level.Option{
		level.AllowWarn(),
		level.ErrNotAllowed(myError),
	}
	logger := level.NewDynamicFilter(log.NewNopLogger(), opts...)

	const expiration = 100 * time.Millisecond

	_, file, line, _ := runtime.Caller(0)
	logger.Override(file, line+4, level.LogAlways, expiration)
	logger.Override(file, line+8, level.LogNever, expiration)

	if want, have := error(nil), level.Info(logger).Log("foo", "bar"); want != have {
		t.Errorf("want %#+v, have %#+v", want, have)
	}

	if want, have := myError, level.Warn(logger).Log("foo", "bar"); want != have {
		t.Errorf("want %#+v, have %#+v", want, have)
	}

	if o := logger.Overrides(); len(o) != 2 {
		t.Errorf("unexpected overrides %#+v", o)
	}

	time.Sleep(2 * expiration)

	if want, have := myError, level.Info(logger).Log("foo", "bar"); want != have {
		t.Errorf("want %#+v, have %#+v", want, have)
	}

	if want, have := error(nil), level.Warn(logger).Log("foo", "bar"); want != have {
		t.Errorf("want %#+v, have %#+v", want, have)
	}

	if o := logger.Overrides(); !reflect.DeepEqual(o, []level.Override{}) {
		t.Errorf("unexpected overrides %#+v", o)
	}
}
