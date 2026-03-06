package chat

import (
	"sync"
	"time"
)

// CronJob is a scheduled recurring task.
type CronJob struct {
	ID       string
	Task     string
	Interval time.Duration
	Strategy string // force a strategy, or "" to auto-classify
	Enabled  bool

	lastRun time.Time
	running bool
}

// CronHandler is called when a scheduled job fires.
type CronHandler func(job *CronJob)

// Cron manages scheduled recurring tasks.
type Cron struct {
	mu      sync.Mutex
	jobs    map[string]*CronJob
	handler CronHandler
	stop    chan struct{}
	running bool
}

func newCron(handler CronHandler) *Cron {
	return &Cron{
		jobs:    make(map[string]*CronJob),
		handler: handler,
		stop:    make(chan struct{}),
	}
}

func (c *Cron) Add(job *CronJob) {
	c.mu.Lock()
	defer c.mu.Unlock()
	job.Enabled = true
	c.jobs[job.ID] = job
}

func (c *Cron) Remove(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.jobs, id)
}

func (c *Cron) Pause(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if j, ok := c.jobs[id]; ok {
		j.Enabled = false
	}
}

func (c *Cron) Resume(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if j, ok := c.jobs[id]; ok {
		j.Enabled = true
	}
}

func (c *Cron) List() []*CronJob {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*CronJob, 0, len(c.jobs))
	for _, j := range c.jobs {
		out = append(out, j)
	}
	return out
}

func (c *Cron) Start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()

	go c.loop()
}

func (c *Cron) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return
	}
	c.running = false
	close(c.stop)
}

func (c *Cron) loop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stop:
			return
		case now := <-ticker.C:
			c.tick(now)
		}
	}
}

func (c *Cron) tick(now time.Time) {
	c.mu.Lock()
	var ready []*CronJob
	for _, j := range c.jobs {
		if !j.Enabled || j.running {
			continue
		}
		if j.lastRun.IsZero() || now.Sub(j.lastRun) >= j.Interval {
			j.running = true
			j.lastRun = now
			ready = append(ready, j)
		}
	}
	c.mu.Unlock()

	for _, j := range ready {
		go func(job *CronJob) {
			c.handler(job)
			c.mu.Lock()
			job.running = false
			c.mu.Unlock()
		}(j)
	}
}
