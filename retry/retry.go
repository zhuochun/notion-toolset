package retry

import "time"

const Count = 5
const Delay = time.Second

func Do(fn func() error) error {
	return DoIf(fn, func(error) bool { return true })
}

func DoIf(fn func() error, shouldRetry func(error) bool) error {
	return doIf(fn, shouldRetry, time.Sleep)
}

func doIf(fn func() error, shouldRetry func(error) bool, wait func(time.Duration)) error {
	var err error
	delay := Delay

	for attempt := 0; attempt < Count; attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		if !shouldRetry(err) || attempt == Count-1 {
			return err
		}

		wait(delay)
		delay *= 2
	}

	return err
}
