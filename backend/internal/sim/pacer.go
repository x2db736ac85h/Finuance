package sim

import "time"

// pacer is one schedule shared by many (the generic cell rate algorithm): each turn comes an
// interval after the one before, and a pacer that has been quiet lets up to depth turns through at
// once. Load mode's lab-wide target, the personas' sign-ins and the feed's sampling use one each.
// The caller holds the lock that guards it.
type pacer struct{ next time.Time }

// catchUp moves a schedule that fell behind to where depth turns are free now.
func (p *pacer) catchUp(now time.Time, interval time.Duration, depth float64) {
	if earliest := now.Add(interval - time.Duration(depth*float64(interval))); p.next.Before(earliest) {
		p.next = earliest
	}
}

// reserve takes the next turn and returns when it is (now, if it is free already); the turn after
// it comes step later. depth ≥ 1.
func (p *pacer) reserve(now time.Time, interval, step time.Duration, depth float64) time.Time {
	p.catchUp(now, interval, depth)
	t := p.next
	p.next = p.next.Add(step)
	if t.Before(now) {
		return now
	}
	return t
}

// allow takes a turn only if one is free now.
func (p *pacer) allow(now time.Time, interval time.Duration, depth float64) bool {
	p.catchUp(now, interval, depth)
	if p.next.After(now) {
		return false
	}
	p.next = p.next.Add(interval)
	return true
}
