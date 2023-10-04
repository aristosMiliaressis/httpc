package httpc

type MessageLog []*MessageDuplex

func (log MessageLog) Find(where func(evt *MessageDuplex) bool) *MessageDuplex {
	for _, evt := range log {
		if where(evt) {
			return evt
		}
	}

	return nil
}

func (log MessageLog) Search(where func(evt *MessageDuplex) bool) MessageLog {
	found := MessageLog{}
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

func (log MessageLog) Select(filter func(evt *MessageDuplex) string) []string {
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
