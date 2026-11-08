package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
)

// JobAccepted is the 202 answer of every long operation (spec 36.1,
// AGENTS.md): the job id, the record as it is now, and where to follow it.
// The operation continues after the response; closing the page does not
// stop it (ENG-001).
type JobAccepted struct {
	JobID string    `json:"jobId"`
	Job   *jobs.Job `json:"job"`
	Links JobLinks  `json:"links"`
	// Deduplicated: the requestId was already accepted and this is that
	// job, not a new one (ENG-002).
	Deduplicated bool `json:"deduplicated"`
	// Stopped is sent by cancel: false means cancellation was requested and
	// the job has not stopped yet.
	Stopped *bool `json:"stopped,omitempty"`
}

// JobLinks are the follow-up operations of a job.
type JobLinks struct {
	Self   string `json:"self"`
	Events string `json:"events"`
	Cancel string `json:"cancel"`
}

func jobLinks(id string) JobLinks {
	p := "/management/v1/jobs/" + id
	return JobLinks{Self: p, Events: p + "/events", Cancel: p + "/cancel"}
}

// acceptJob answers 202 with the job and a Location header.
func acceptJob(c *gin.Context, job *jobs.Job, dedup bool) { acceptJobWith(c, job, dedup, nil) }

func acceptJobWith(c *gin.Context, job *jobs.Job, dedup bool, stopped *bool) {
	l := jobLinks(job.ID)
	c.Header("Location", l.Self)
	c.JSON(http.StatusAccepted, JobAccepted{JobID: job.ID, Job: job, Links: l, Deduplicated: dedup, Stopped: stopped})
}
