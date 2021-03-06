package retry

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

var (
	errRetriable = errors.New("retriable")
)

func isRetriable(e error) bool {
	return e == errRetriable
}

func TestRetry(t *testing.T) {
	retryInitialSleepAmount = 10 * time.Millisecond
	retryMaxSleepAmount = 20 * time.Millisecond
	maxAttempts = 3

	cnt := 0

	cases := []struct {
		desc      string
		f         func() (interface{}, error)
		want      interface{}
		wantError error
	}{
		{"success-nil", func() (interface{}, error) { return nil, nil }, nil, nil},
		{"success", func() (interface{}, error) { return 3, nil }, 3, nil},
		{"retriable-succeeds", func() (interface{}, error) {
			cnt++
			if cnt < 2 {
				return nil, errRetriable
			}
			return 4, nil
		}, 4, nil},
		{"retriable-never-succeeds", func() (interface{}, error) { return nil, errRetriable }, nil, fmt.Errorf("unable to complete retriable-never-succeeds despite 3 retries")},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			tc := tc
			t.Parallel()

			got, err := WithExponentialBackoff(tc.desc, tc.f, isRetriable)
			if !reflect.DeepEqual(err, tc.wantError) {
				t.Errorf("invalid error %q, wanted %q", err, tc.wantError)
			}

			if got != tc.want {
				t.Errorf("invalid value %v, wanted %v", got, tc.want)
			}
		})
	}
}
