package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v3"
)

type MetricValue struct {
	name  string
	value int
}

type MetricValues struct {
	timestamp time.Time
	values    []MetricValue
}

type Reporter struct {
	values []MetricValues
}

func (reporter *Reporter) sendResult(result MetricValues) {
	reporter.values = append(reporter.values, result)
}

func (reporter *Reporter) report() {
	log.Println("=[ report ]==================")
	for _, value := range reporter.values {
		log.Println(" ", value.timestamp.Format(time.RFC3339), value.values)
	}
	log.Println("=[ end ]=====================")
}

type Eventer struct {
	gatherer GathererInt
	reporter *Reporter
	stoper   func()
}

func (eventer *Eventer) Fire() {
	result, ok := eventer.gatherer.gatherAndCheck(time.Now())
	eventer.reporter.sendResult(result)
	if !ok {
		eventer.stoper()
	}
}

type EnvManager interface {
	start()
	stop()
}

type DockerCompose struct {
	workDir           string
	dockerComposeFile string
}

func osexec(logMsg string, workDir string, args ...string) {
	log.Println(logMsg)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = workDir
	if err := cmd.Run(); err != nil {
		log.Fatalln(err)
	}
}

func (envManager DockerCompose) start() {
	osexec("start stand", envManager.workDir, "docker", "compose", "up", "-d", "--remove-orphans")
}

func (envManager DockerCompose) stop() {
	osexec("stop stand", envManager.workDir, "docker", "compose", "down")
}

type Metric struct {
	Name     string `yaml:"name"`
	Query    string `yaml:"query"`
	MaxValue int    `yaml:"maxValue"`
}

type GathererInt interface {
	gatherAndCheck(startTime time.Time) (MetricValues, bool)
}

type Gatherer struct {
	metrics []MetricGather
	host    string
}

type MetricGather interface {
	gather() int
	name() string
	maxValue() int
}

type PrometheusMetric struct {
	Host     string
	Name     string
	Query    string
	MaxValue int
}

func (metric PrometheusMetric) name() string {
	return metric.Name
}

func (metric PrometheusMetric) maxValue() int {
	return metric.MaxValue
}

func (metric PrometheusMetric) gather() int {
	client, err := api.NewClient(api.Config{
		Address: metric.Host,
	})
	if err != nil {
		fmt.Printf("Error creating client: %v\n", err)
		return -1
	}

	v1api := v1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	val, warnings, err := v1api.Query(ctx, metric.Query, time.Now(), v1.WithTimeout(5*time.Second))
	if err != nil {
		log.Printf("Error querying Prometheus: %v\n", err)
		return -1
	}
	if len(warnings) > 0 {
		log.Printf("Warnings: %v\n", warnings)
	}
	switch {
	case val.Type() == model.ValVector:
		vectorVal := val.(model.Vector)
		if vectorVal.Len() != 1 {
			log.Println("WARNING: too many values ", vectorVal.Len())
			return -1
		}
		for _, elem := range vectorVal {
			return int(elem.Value)
		}
	default:
		log.Printf("   Result:\n%v\n", val)
		panic(val.Type().String())
	}
	return -1
}

func (gatherer Gatherer) gatherAndCheck(startTime time.Time) (MetricValues, bool) {
	flag := true
	metricValues := MetricValues{timestamp: startTime, values: []MetricValue{}}
	for _, metric := range gatherer.metrics {
		value := metric.gather()
		metricValues.values = append(metricValues.values, MetricValue{name: metric.name(), value: value})
		if value > metric.maxValue() {
			log.Println(" metric("+metric.name()+"):", value, ">", metric.maxValue())
			flag = false
		}
	}
	return metricValues, flag
}

type EventerInt interface {
	Fire()
}

type EnvManagerInt interface {
	start()
	stop()
}

type Scheduler struct {
	envManager   EnvManagerInt
	eventer      EventerInt
	status       int
	startDelay   int
	testDuration int
	timeout      int
}

func (scheduler Scheduler) init() {
	scheduler.envManager.start()
}

func (scheduler Scheduler) down() {
	scheduler.envManager.stop()
}

func (scheduler *Scheduler) sendDown() {
	if scheduler.status != 0 {
		return
	}
	scheduler.status = 1
}

func (scheduler *Scheduler) tick() {
	if scheduler.status != 0 {
		return
	}
	scheduler.eventer.Fire()
}

type Config struct {
	Host         string   `yaml:"host"`
	Metrics      []Metric `yaml:"metrics"`
	StartDelay   int      `yaml:"startDelay"`
	TestDuration int      `yaml:"testDuration"`
	WorkDir      string   `yaml:"workDir"`
	Timeout      int      `yaml:"timeout"`
}

type App struct {
}

func (app App) run() {
	config, err := app.loadConfig("./config.yaml")
	if err != nil {
		log.Fatalln(err)
		return
	}
	log.Println("=[ info ]==============================")
	log.Println("      workDir:", config.WorkDir)
	log.Println("   startDelay:", config.StartDelay)
	log.Println(" testDuration:", config.TestDuration)
	log.Println("      timeout:", config.Timeout)
	log.Println("=[ init ]==============================")

	reporter := Reporter{}
	scheduler := app.tune(reporter, config)
	scheduler.init()

	scheduler.run()
	log.Println("=[ stop ]==============================")
	scheduler.down()
	reporter.report()
}

func (App) tune(reporter Reporter, config Config) Scheduler {

	metrics := make([]MetricGather, 0)
	for _, metric := range config.Metrics {
		metrics = append(metrics, PrometheusMetric{
			Host: config.Host,
			Name: metric.Name, Query: metric.Query,
			MaxValue: metric.MaxValue})
	}

	eventer := Eventer{
		reporter: &reporter,
		gatherer: Gatherer{
			host:    config.Host,
			metrics: metrics,
		},
	}
	scheduler := Scheduler{
		envManager: DockerCompose{
			workDir:           config.WorkDir,
			dockerComposeFile: "docker-compose.yaml",
		},
		eventer:      &eventer,
		status:       0,
		startDelay:   config.StartDelay,
		testDuration: config.TestDuration,
		timeout:      config.Timeout,
	}
	eventer.stoper = func() { scheduler.sendDown() }
	return scheduler
}

func (App) loadConfig(fileName string) (Config, error) {
	b, err := os.ReadFile(fileName)
	if err != nil {
		log.Fatalln(err)
		return Config{}, err
	}
	config := Config{}
	if err := yaml.Unmarshal(b, &config); err != nil {
		log.Fatalln(err)
		return Config{}, err
	}
	return config, nil
}

func (scheduler *Scheduler) run() {
	log.Println("=[ delay ]=============================")
	time.Sleep(time.Duration(scheduler.startDelay) * time.Second)
	log.Println("=[ start gathers ]=====================")
	ctx := context.Background()
	ctx, cancelFunc := context.WithTimeout(ctx, time.Duration(scheduler.testDuration)*time.Second)
	defer cancelFunc()

	for {
		select {
		case <-ctx.Done():
			log.Println("=[ timeout ]============================")
			return
		default:
			if scheduler.status == 1 {
				return
			}
			scheduler.tick()
			time.Sleep(time.Duration(scheduler.timeout) * time.Second)
		}
	}
}

func main() {
	App{}.run()
}
