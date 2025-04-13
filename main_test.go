package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPrometheusMetric(t *testing.T) {
	requires := require.New(t)
	metric := PrometheusMetric{Name: "a", MaxValue: 1}
	requires.Equal("a", metric.name())
	requires.Equal(1, metric.maxValue())
}

func TestPrometheusMetricGather(t *testing.T) {
	requires := require.New(t)
	metric := PrometheusMetric{Name: "a", MaxValue: 1}
	requires.Equal(-1, metric.gather())
}

func TestSendResult(t *testing.T) {
	requires := require.New(t)
	reporter := Reporter{}
	reporter.sendResult(MetricValues{})
	requires.Len(reporter.values, 1)
}

type FakeGatherer struct {
}

func (gatherer FakeGatherer) gatherAndCheck(startTime time.Time) (MetricValues, bool) {
	return MetricValues{}, false
}

func TestEventerFire(t *testing.T) {
	requires := require.New(t)
	counter := 0
	eventer := Eventer{
		gatherer: FakeGatherer{},
		reporter: &Reporter{},
		stoper: func() {
			counter++
		}}
	eventer.Fire()
	requires.Equal(1, counter)
}

type FakeMetricGather struct {
}

func (m FakeMetricGather) name() string  { return "a" }
func (m FakeMetricGather) gather() int   { return 2 }
func (m FakeMetricGather) maxValue() int { return 1 }

func TestGathererGatherAndCheck(t *testing.T) {
	requires := require.New(t)
	gatherer := Gatherer{metrics: []MetricGather{
		FakeMetricGather{},
	}}
	startTime := time.Now()
	values, check := gatherer.gatherAndCheck(startTime)
	requires.Equal(values.timestamp, startTime)
	requires.Len(values.values, 1)
	requires.False(check)
}

func TestSchedulerSendDown(t *testing.T) {
	requires := require.New(t)
	scheduler := Scheduler{}
	scheduler.sendDown()
	requires.Equal(scheduler.status, 1)
}

type FakeEventer struct {
	fired  int
	stoper func()
}

func (eventer *FakeEventer) Fire() {
	eventer.fired = eventer.fired + 1
	if eventer.stoper != nil {
		eventer.stoper()
	}
}

func TestSchedulerTick(t *testing.T) {
	requires := require.New(t)
	fakeEventer := FakeEventer{}
	scheduler := Scheduler{eventer: &fakeEventer}
	scheduler.tick()
	requires.Equal(fakeEventer.fired, 1)
}

type FakeEnvManager struct {
	started bool
	stopped bool
}

func (env *FakeEnvManager) start() {
	env.started = true
}

func (env *FakeEnvManager) stop() {
	env.stopped = true
}

func TestSchedulerInitAndDown(t *testing.T) {
	env := FakeEnvManager{}
	scheduler := Scheduler{envManager: &env}
	scheduler.init()
	scheduler.down()
	requires := require.New(t)
	requires.True(env.started)
	requires.True(env.stopped)
}

func TestSchedulerRun(t *testing.T) {
	variants := []struct {
		testDuration int
		result       int
	}{
		{
			testDuration: 0,
			result:       0,
		},
		{
			testDuration: 1,
			result:       1,
		},
	}
	requires := require.New(t)
	for n, variant := range variants {
		fakeEventer := FakeEventer{}
		scheduler := Scheduler{
			eventer:      &fakeEventer,
			testDuration: variant.testDuration,
		}
		fakeEventer.stoper = func() { scheduler.sendDown() }
		scheduler.run()
		requires.Equal(variant.result, fakeEventer.fired, n)
	}
}

func TestAppLoadConfig(t *testing.T) {
	variants := []struct {
		content string
		config  Config
	}{
		{
			content: "workDir: /tmp/project",
			config:  Config{WorkDir: "/tmp/project"},
		},
	}
	requires := require.New(t)
	for _, variant := range variants {
		a := App{}
		dir := t.TempDir()
		fileName := filepath.Join(dir, "config.yaml")
		requires.NoError(os.WriteFile(fileName, []byte(variant.content), fs.ModePerm))
		config, _ := a.loadConfig(fileName)
		requires.Equal(variant.config.WorkDir, config.WorkDir)
	}
}

func TestAppTune(t *testing.T) {
	variants := []struct {
		config Config
	}{
		{
			config: Config{WorkDir: "/tmp/project"},
		},
	}
	requires := require.New(t)
	for _, variant := range variants {
		a := App{}
		scheduler := a.tune(Reporter{}, variant.config)
		requires.Equal(0, scheduler.status)
		requires.Equal(0, scheduler.startDelay)   //   config.StartDelay,
		requires.Equal(0, scheduler.testDuration) // config.TestDuration,
		requires.Equal(0, scheduler.timeout)      //      config.Timeout,
		// o, err := yaml.Marshal(scheduler)
		// requires.NoError(err)
		// fmt.Println("result:", string(o))
		// requires.Equal(scheduler.eventer.gatherer.host, "http://localhost:9090")
		// requires.Equal(DockerCompose(scheduler.envManager).WorkDir, "/tmp/project")
	}
}
