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

func (log EventLog) Search(where func(evt *HttpEvent) bool) []*HttpEvent {
	found := []*HttpEvent{}
	for _, evt := range log {
		for {
			if where(evt) {
				found = append(found, evt)
			}

			if evt.Prev == nil {
				break
			}

			evt = evt.Prev
		}
	}

	return found
}
