package queue

// the smoke tests cover most operations of a queue under a number of
// different situations. these tests just fill in the gaps. and help us ensure
// consistent behavior of this implementation

import (
	"fmt"
	"testing"

	"github.com/mongodb/amboy"
	"github.com/mongodb/amboy/job"
	"github.com/mongodb/amboy/pool"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"golang.org/x/net/context"
)

type ShuffledQueueSuite struct {
	require *require.Assertions
	queue   *LocalShuffled
	suite.Suite
}

func TestShuffledQueueSuite(t *testing.T) {
	suite.Run(t, new(ShuffledQueueSuite))
}

func (s *ShuffledQueueSuite) SetupSuite() {
	s.require = s.Require()
}

func (s *ShuffledQueueSuite) SetupTest() {
	s.queue = &LocalShuffled{}
}

func (s *ShuffledQueueSuite) TestCannotStartQueueWithNilRunner() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// make sure that unstarted queues without runners will error
	// if you attempt to set them
	s.False(s.queue.Started())
	s.Nil(s.queue.runner)
	s.Error(s.queue.Start(ctx))
	s.False(s.queue.Started())

	// now validate the inverse
	s.NoError(s.queue.SetRunner(pool.NewSingleRunner()))
	s.NotNil(s.queue.runner)
	s.NoError(s.queue.Start(ctx))
	s.True(s.queue.Started())
}

func (s *ShuffledQueueSuite) TestPutFailsWithUnstartedQueue() {
	s.False(s.queue.Started())
	s.Error(s.queue.Put(job.NewShellJob("echo 1", "")))

	// now validate the inverse
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.NoError(s.queue.SetRunner(pool.NewSingleRunner()))
	s.NoError(s.queue.Start(ctx))
	s.True(s.queue.Started())

	s.NoError(s.queue.Put(job.NewShellJob("echo 1", "")))
}

func (s *ShuffledQueueSuite) TestPutFailsIfJobIsTracked() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.NoError(s.queue.SetRunner(pool.NewSingleRunner()))
	s.NoError(s.queue.Start(ctx))

	j := job.NewShellJob("echo 1", "")

	// first, attempt works fine
	s.NoError(s.queue.Put(j))

	// afterwords, attempts should fail
	for i := 0; i < 10; i++ {
		s.Error(s.queue.Put(j))
	}
}

func (s *ShuffledQueueSuite) TestStatsShouldReturnNilObjectifQueueIsNotRunning() {
	s.False(s.queue.Started())
	for i := 0; i < 20; i++ {
		s.Equal(amboy.QueueStats{}, s.queue.Stats())
	}
}

func (s *ShuffledQueueSuite) TestSetRunnerReturnsErrorIfRunnerHasStarted() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.NoError(s.queue.SetRunner(pool.NewSingleRunner()))
	s.NoError(s.queue.Start(ctx))
	origRunner := s.queue.Runner()

	s.Error(s.queue.SetRunner(pool.NewSingleRunner()))

	s.Exactly(origRunner, s.queue.Runner())
}

func (s *ShuffledQueueSuite) TestGetMethodRetrieves() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	j := job.NewShellJob("true", "")

	jReturn, ok := s.queue.Get(j.ID())
	s.False(ok)
	s.Nil(jReturn)

	s.NoError(s.queue.SetRunner(pool.NewSingleRunner()))
	s.NoError(s.queue.Start(ctx))

	jReturn, ok = s.queue.Get(j.ID())
	s.False(ok)
	s.Nil(jReturn)

	s.NoError(s.queue.Put(j))

	jReturn, ok = s.queue.Get(j.ID())
	s.True(ok)
	s.Exactly(jReturn, j)
	amboy.Wait(s.queue)

	jReturn, ok = s.queue.Get(j.ID())
	s.True(ok)
	s.Exactly(jReturn, j)
}

func (s *ShuffledQueueSuite) TestResultsOperationReturnsEmptyChannelIfQueueIsNotStarted() {
	s.False(s.queue.Started())
	count := 0
	fmt.Printf("%+v", s.queue)

	for range s.queue.Results() {
		count++
	}

	s.Equal(0, count)
}

func (s *ShuffledQueueSuite) TestCompleteReturnsIfContextisCanceled() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.NoError(s.queue.SetRunner(pool.NewSingleRunner()))
	s.NoError(s.queue.Start(ctx))

	ctx2, cancel2 := context.WithCancel(ctx)
	j := job.NewShellJob("false", "")
	cancel2()
	s.queue.Complete(ctx2, j)
	stat := s.queue.Stats()
	s.Equal(0, stat.Completed)
}
