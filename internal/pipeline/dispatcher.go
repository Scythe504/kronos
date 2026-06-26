package pipeline

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
)

func (p *Pipeline) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		tasks, err := p.db.GetTask(ctx)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				time.Sleep(1 * time.Second)
				continue
			}

			log.Println("ERR(Dispatcher): ", err)
			time.Sleep(2 * time.Second)
			continue
		}
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
