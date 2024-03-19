package httpc

type MessageLog []*MessageDuplex

func (log MessageLog) Find(where func(msg *MessageDuplex) bool) *MessageDuplex {
	for _, msg := range log {
		if where(msg) {
			return msg
		}
	}

	return nil
}

func (log MessageLog) Search(where func(msg *MessageDuplex) bool) MessageLog {
	found := MessageLog{}
	for _, msg := range log {
		for {
			if where(msg) {
				found = append(found, msg)
			}

			if msg.Prev == nil {
				break
			}

			msg = msg.Prev
		}
	}

	return found
}

func (log MessageLog) Select(filter func(msg *MessageDuplex) string) []string {
	selected := []string{}
	for _, msg := range log {
		for {
			selected = append(selected, filter(msg))

			if msg.Prev == nil {
				break
			}

			msg = msg.Prev
		}
	}

	return selected
}

func (log MessageLog) SelectInt64(filter func(msg *MessageDuplex) int64) []int64 {
	selected := []int64{}
	for _, msg := range log {
		for {
			selected = append(selected, filter(msg))

			if msg.Prev == nil {
				break
			}

			msg = msg.Prev
		}
	}

	return selected
}
