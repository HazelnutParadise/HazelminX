package jobs

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// reporter is the bounded channel between a running step and the record.
// Log lines go to a fixed-size tail (and to events up to a cap); heartbeat
// and progress are cheap store touches that a log flood cannot delay.
type reporter struct {
	e     *Engine
	job   *Job
	step  int
	mu    sync.Mutex
	tail  []string
	drop  int
	sent  int
	evid  []string
	prog  int
	beat  bool
	trunc bool
	last  time.Time // last store touch; touches are throttled to one per second
}

func newReporter(e *Engine, j *Job, step int) *reporter {
	return &reporter{e: e, job: j, step: step, prog: j.Progress}
}

func (r *reporter) Log(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.tail) >= r.e.opts.LogTail {
		r.tail = r.tail[1:]
		r.drop++
	}
	r.tail = append(r.tail, line)
	if r.sent < r.e.opts.MaxLogEvents {
		r.sent++
		r.e.event(context.Background(), r.job, Event{Type: EventLog, Step: r.step, Message: line})
		return
	}
	if !r.trunc {
		r.trunc = true
		r.e.event(context.Background(), r.job, Event{Type: EventLogTruncated, Step: r.step, Message: "further log lines are kept only as a tail; count is recorded on the step"})
	}
}

func (r *reporter) Progress(percent int) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	r.mu.Lock()
	changed := percent != r.prog
	r.prog = percent
	r.mu.Unlock()
	if changed {
		data, _ := json.Marshal(map[string]int{"percent": percent})
		r.e.event(context.Background(), r.job, Event{Type: EventProgress, Step: r.step, Data: data})
		r.touch()
	}
}

func (r *reporter) Heartbeat() {
	r.mu.Lock()
	r.beat = true
	r.mu.Unlock()
	r.touch()
}

func (r *reporter) touch() {
	now := r.e.now()
	r.mu.Lock()
	p := r.prog
	if now.Sub(r.last) < time.Second {
		r.mu.Unlock()
		return
	}
	r.last = now
	r.mu.Unlock()
	if err := r.e.store.Touch(context.Background(), r.job.ID, now, p); err != nil {
		r.e.opts.Logger.Error("jobs: heartbeat", "job", r.job.ID, "error", err)
	}
}

func (r *reporter) Evidence(ref string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evid = append(r.evid, ref)
}

// flushInto copies what the step reported onto the record once Execute has
// returned, so the job struct is only written by the driving goroutine.
func (r *reporter) flushInto(step *Step, j *Job) {
	r.mu.Lock()
	defer r.mu.Unlock()
	step.LogTail = append([]string(nil), r.tail...)
	step.LogsDropped = r.drop
	step.Evidence = append(step.Evidence, r.evid...)
	j.Evidence = append(j.Evidence, r.evid...)
	j.Progress = r.prog
	if r.beat {
		now := r.e.now()
		j.LastHeartbeatAt = &now
	}
}
