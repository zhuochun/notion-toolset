package retry

import "time"

const Count = 3
const Delay = time.Second

func Do(fn func() error) error {
	var err error
	for i := 0; i < Count; i++ {
		if err = fn(); err == nil {
			return nil
		}
		time.Sleep(Delay)
	}
	return err
}
