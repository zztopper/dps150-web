package notify

import (
	"fmt"
	"sync"
	"time"
)

// throttleState is the per-kind antispam state. The first notification of a
// kind goes out immediately; anything else within the cooldown window is
// aggregated — a flush timer fires once the window closes and sends the
// latest suppressed text with a "повторилось N раз" suffix, opening the next
// window.
type throttleState struct {
	mu      sync.Mutex
	stopped bool
	kinds   map[Kind]*kindState
}

// kindState tracks one notification kind; throttleState.mu guards it.
type kindState struct {
	lastSent time.Time
	pending  int         // suppressed notifications since lastSent
	lastText string      // text of the latest suppressed notification
	flush    *time.Timer // pending window-close flush, nil when none
}

// throttleNotify sends text now when the kind is outside its cooldown
// window, otherwise schedules an aggregated flush for the window close.
func (s *Service) throttleNotify(kind Kind, text string) {
	s.throttle.mu.Lock()
	defer s.throttle.mu.Unlock()
	if s.throttle.stopped {
		return
	}
	st := s.throttle.kinds[kind]
	if st == nil {
		st = &kindState{}
		s.throttle.kinds[kind] = st
	}
	now := time.Now()
	if st.pending == 0 && now.Sub(st.lastSent) >= s.cooldown {
		st.lastSent = now
		s.enqueue(text)
		return
	}
	st.pending++
	st.lastText = text
	if st.flush == nil {
		wait := s.cooldown - now.Sub(st.lastSent)
		if wait < 0 {
			wait = 0
		}
		st.flush = time.AfterFunc(wait, func() { s.flushKind(kind) })
	}
}

// flushKind closes a cooldown window: it sends the latest suppressed text of
// the kind — annotated with the repeat count when more than one notification
// was collapsed — and starts the next window.
func (s *Service) flushKind(kind Kind) {
	s.throttle.mu.Lock()
	defer s.throttle.mu.Unlock()
	if s.throttle.stopped {
		return
	}
	st := s.throttle.kinds[kind]
	if st == nil {
		return
	}
	st.flush = nil
	if st.pending == 0 {
		return
	}
	text := st.lastText
	if st.pending > 1 {
		text = fmt.Sprintf("%s (повторилось %d раз)", text, st.pending)
	}
	st.pending = 0
	st.lastSent = time.Now()
	s.enqueue(text)
}

// stopThrottle cancels the pending flush timers; Run calls it on exit.
func (s *Service) stopThrottle() {
	s.throttle.mu.Lock()
	defer s.throttle.mu.Unlock()
	s.throttle.stopped = true
	for _, st := range s.throttle.kinds {
		if st.flush != nil {
			st.flush.Stop()
			st.flush = nil
		}
	}
}
