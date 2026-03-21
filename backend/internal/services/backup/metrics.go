package backup

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    metricRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Namespace: "aggregator",
        Name:      "backup_runs_total",
        Help:      "Total number of backup runs by status.",
    }, []string{"status"})

    metricRunDuration = promauto.NewHistogram(prometheus.HistogramOpts{
        Namespace: "aggregator",
        Name:      "backup_run_duration_seconds",
        Help:      "Backup run duration in seconds.",
        Buckets:   prometheus.DefBuckets,
    })

    metricUploadedBytes = promauto.NewCounter(prometheus.CounterOpts{
        Namespace: "aggregator",
        Name:      "backup_uploaded_bytes_total",
        Help:      "Total uploaded bytes across backup runs.",
    })

    metricLastSuccess = promauto.NewGaugeVec(prometheus.GaugeOpts{
        Namespace: "aggregator",
        Name:      "backup_last_success_timestamp",
        Help:      "Unix timestamp of the last successful run per job.",
    }, []string{"job_id"})

    metricJobEnabled = promauto.NewGauge(prometheus.GaugeOpts{
        Namespace: "aggregator",
        Name:      "backup_job_enabled_total",
        Help:      "Number of enabled backup jobs.",
    })

    metricRunItemsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Namespace: "aggregator",
        Name:      "backup_run_items_total",
        Help:      "Backup run items by status.",
    }, []string{"status"})
)
