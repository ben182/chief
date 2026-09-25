package headless

import (
	"sync"

	"github.com/ben182/chief/internal/git"
)

// storyPusher pushes the run's branch each time a story ends, for a run whose
// machine does not outlive it. The push at the end of the run is the one that
// is meant to take the work off the box, and it only happens if the run gets
// there: a box that dies at hour nine, or a deadline that ends a run the reaper
// then cannot rescue, would take every story before it along. Pushed per story,
// the most that can be lost is the story in progress.
//
// Pushing happens beside the run, not in it: a push that takes a while, or
// fails, must not hold up the event stream the loop feeds. Stories that end
// while a push is under way are covered by one more push after it, not one each.
type storyPusher struct {
	requests chan string
	done     sync.WaitGroup
	push     func(dir, branch string) error
}

// newStoryPusher starts pushing branch from dir on request. Nil when there is
// nothing to push to, so callers need no check of their own.
func newStoryPusher(log *logger, dir, branch string) *storyPusher {
	if branch == "" {
		return nil
	}
	return startStoryPusher(log, dir, branch, git.PushBranch)
}

func startStoryPusher(log *logger, dir, branch string, push func(dir, branch string) error) *storyPusher {
	p := &storyPusher{requests: make(chan string, 1), push: push}
	p.done.Add(1)
	go func() {
		defer p.done.Done()
		for story := range p.requests {
			if err := p.push(dir, branch); err != nil {
				log.event("push", "after %s failed: %v — the run carries on, the next story tries again", story, err)
				continue
			}
			log.event("push", "%s pushed after %s", branch, story)
		}
	}()
	return p
}

// storyEnded asks for a push. It never blocks: with one already waiting, that
// one will carry this story's commits as well.
func (p *storyPusher) storyEnded(story string) {
	if p == nil {
		return
	}
	select {
	case p.requests <- story:
	default:
	}
}

// stop waits for the push under way, if any, and ends the pusher. The final
// push of the run comes after it, so the two never race.
func (p *storyPusher) stop() {
	if p == nil {
		return
	}
	close(p.requests)
	p.done.Wait()
}
