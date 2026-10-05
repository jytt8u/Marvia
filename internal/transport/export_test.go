package transport

import "time"

// SetRealityClock подменяет часы клиента REALITY — чтобы проверить, как нода
// встречает приветствие со сбитым или старым временем, не дожидаясь его.
func SetRealityClock(now func() time.Time) (restore func()) {
	was := realityNow
	realityNow = now
	return func() { realityNow = was }
}
