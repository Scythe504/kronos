package telemetry

type Metric struct {
	Name        string
	Unit        string
	Description string
}

var (
	// Task Metrics
	MetricTasksPulled = Metric{
		Name:        "kronos_tasks_pulled_total",
		Unit:        "1",
		Description: "Total number of tasks pulled/leased by nodes",
	}
	MetricTasksFailed = Metric{
		Name:        "kronos_tasks_failed_total",
		Unit:        "1",
		Description: "Total number of tasks that failed execution",
	}
	MetricTaskExecutionDuration = Metric{
		Name:        "kronos_task_execution_duration_seconds",
		Unit:        "s",
		Description: "Histogram of task execution durations in seconds",
	}
	MetricTaskQueueDuration = Metric{
		Name:        "kronos_task_queue_duration_seconds",
		Unit:        "s",
		Description: "Time tasks spend in queued state before being leased",
	}
	MetricTaskRetries = Metric{
		Name:        "kronos_task_retries_total",
		Unit:        "1",
		Description: "Total number of task execution retries",
	}

	// Worker Metrics
	MetricWorkerSpawnDuration = Metric{
		Name:        "kronos_worker_spawn_duration_seconds",
		Unit:        "s",
		Description: "Time taken to spawn and establish pipes for a worker process",
	}
	MetricActiveWorkers = Metric{
		Name:        "kronos_active_workers",
		Unit:        "1",
		Description: "Current number of active worker processes running on the node",
	}

	// System Resource Metrics
	MetricNodeCPUUtilization = Metric{
		Name:        "kronos_node_cpu_utilization",
		Unit:        "percent",
		Description: "Current node CPU utilization percentage",
	}
	MetricNodeMemoryUsed = Metric{
		Name:        "kronos_node_memory_used_bytes",
		Unit:        "By",
		Description: "Current node memory usage in bytes",
	}
	MetricNodeGPUUtilization = Metric{
		Name:        "kronos_node_gpu_utilization",
		Unit:        "percent",
		Description: "Current node GPU utilization percentage",
	}
)
