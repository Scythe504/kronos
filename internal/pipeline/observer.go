package pipeline

import (
	"bufio"
	"context"
	"log"
)

func (p *Pipeline) ObserveProcessStdout(ctx context.Context, slug string) {
	pipe, err := p.GetPipe(ctx, slug)
	if err != nil {
		log.Println("ERR(ObserveProcessStdout):", err)
		return
	}
	scanner := bufio.NewScanner(pipe.Stdout)
	for scanner.Scan() {
		log.Printf("\n[%s]:%s\n", slug, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		log.Println("ERR(ObserveProcessStdout):", err)
		return
	}
}

func (p *Pipeline) ObserveProcessStderr(ctx context.Context, slug string) {
	pipe, err := p.GetPipe(ctx, slug)
	if err != nil {
		log.Println("ERR(ObserveProcessStdout):", err)
		return
	}
	scanner := bufio.NewScanner(pipe.Stderr)
	for scanner.Scan() {
		log.Printf("\n[%s]:%s\n", slug, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		log.Println("ERR(ObserveProcessStdout):", err)
		return
	}
}
