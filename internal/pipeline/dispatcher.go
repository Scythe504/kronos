package pipeline

import (
	"context"
	"log"
	"time"

	"github.com/scythe504/kronos/internal/nodes"
)

func (p *Pipeline) Start(ctx context.Context) {
	nodeCfg := nodes.GetNodeConfig(ctx)
	machineID := nodeCfg.MachineID

	pollCount := 1

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		tasks, err := p.db.GetTasks(ctx, machineID, nodeCfg.TaskUnit)
		if err != nil {
			log.Println("ERR(Dispatcher): ", err)
			time.Sleep(2 * time.Second)
			continue
		}

		if len(tasks) == 0 {
			retryCount := min(5, pollCount)
			timeDuration := JitterTime(retryCount).Seconds()
			time.Sleep(time.Duration(timeDuration))
			continue
		}

		// reset pollCount when we get new payloads
		pollCount = 1
		for _, task := range tasks {
			go func() {
				adapted, err := AdaptTask(task)
				if err != nil {
					log.Println(err)
					return
				}

				if err := p.Enqueue(ctx, task.PayloadSlug, adapted); err != nil {
					log.Println("ERR(Enqueued): ", err)
				}
			}()
		}
	}

}
