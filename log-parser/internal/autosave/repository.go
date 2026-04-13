package autosave

import "time"

type State struct {
	CreditAll     int64
	LastAttemptAt time.Time
	DataHash      string
}

type Repository interface {
	Get(userID string) (State, bool, error)
	Update(userID string, state State) error
}
