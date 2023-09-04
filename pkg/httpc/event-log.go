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

func (log EventLog) Search(where func(evt *HttpEvent) bool) EventLog {
	found := EventLog{}
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

func (log EventLog) Select(filter func(evt *HttpEvent) string) []string {
	selected := []string{}
	for _, evt := range log {
		for {
			selected = append(selected, filter(evt))

			if evt.Prev == nil {
				break
			}

			evt = evt.Prev
		}
	}

	return selected
}
