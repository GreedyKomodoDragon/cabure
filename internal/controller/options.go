package controller

import "time"

type OperatorConfig struct {
	WatchNamespace              string
	ConcurrentReconciles        int
	MinimumRequeueInterval      time.Duration
	AllowClusterScopedResources bool
	AllowedRepositoryPrefixes   []string
	FieldManager                string
	CacheDir                    string
}
