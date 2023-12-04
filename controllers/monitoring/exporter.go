package monitoring

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	appv1beta1 "sigs.k8s.io/application/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	loggerCtxKey = "exporterLogger"
)

type Exporter struct {
	options                 Options
	KubePodOwner            *prometheus.Desc
	ExporterLastScrapeError *prometheus.Desc
}

type Options struct {
	Log         logr.Logger
	Client      client.Client
	ConstLabels prometheus.Labels
}

func NewAppExporter(opts Options) (*Exporter, error) {
	return &Exporter{
		options: opts,
		KubePodOwner: prometheus.NewDesc(
			"kube_pod_owner",
			"kube pod owner",
			[]string{"container", "namespace", "owner_is_controller", "owner_kind", "owner_name", "pod"}, opts.ConstLabels,
		),
		ExporterLastScrapeError: prometheus.NewDesc(
			"exporter_last_scrape_error",
			"The last scrape error status.",
			[]string{"err"}, opts.ConstLabels,
		),
	}, nil
}
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.KubePodOwner
	ch <- e.ExporterLastScrapeError
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	collectCtx := context.Background()
	logger := e.options.Log.WithValues("collect", "application")
	ctx := context.WithValue(collectCtx, loggerCtxKey, logger)

	appGVK := appv1beta1.GroupVersion.WithKind(appv1beta1.ResourceKindApplication)

	appList := &appv1beta1.ApplicationList{}
	if err := e.options.Client.List(ctx, appList, &client.ListOptions{}); err != nil {
		logger.Error(err, "unable to appList resources for GVK", "appGVK", appGVK)
		e.registerExporterLastScrapeError(ctx, ch, 1.0, prometheus.GaugeValue, fmt.Sprintf("%s", err))
		return
	}

	for _, application := range appList.Items {
		podList := &v1.PodList{}
		if err := e.options.Client.List(ctx, podList, &client.ListOptions{
			Namespace:     application.Namespace,
			LabelSelector: labels.SelectorFromSet(application.Spec.Selector.MatchLabels),
		}); err != nil {
			logger.Error(err, "unable to appList resources for PodList")
			e.registerExporterLastScrapeError(ctx, ch, 1.0, prometheus.GaugeValue, fmt.Sprintf("%s", err))
			return
		}

		for _, pod := range podList.Items {
			for _, container := range pod.Spec.Containers {
				ch <- prometheus.MustNewConstMetric(e.KubePodOwner, prometheus.CounterValue, 1, container.Name, application.ObjectMeta.Namespace, "true", appGVK.Kind, application.ObjectMeta.Name, pod.Name)
			}
		}
	}
}

func (e *Exporter) registerExporterLastScrapeError(ctx context.Context, ch chan<- prometheus.Metric, val float64, valType prometheus.ValueType, labelValues ...string) {
	logging := getLoggerOrDie(ctx)
	if m, err := prometheus.NewConstMetric(e.ExporterLastScrapeError, valType, val, labelValues...); err == nil {
		ch <- m
	} else {
		logging.Error(err, "unable to register exporter last scrape error")
	}
}

func getLoggerOrDie(ctx context.Context) logr.Logger {
	logger, ok := ctx.Value(loggerCtxKey).(logr.Logger)
	if !ok {
		panic("context didn't contain logger")
	}
	return logger
}
