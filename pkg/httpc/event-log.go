package httpc

type EventLog []*HttpEvent

func (log EventLog) Find(where func(evt *HttpEvent) bool) *HttpEvent {
	for _, evt := range log {
		if where(evt) {
			return evt
		}
	}

	return nil
}
