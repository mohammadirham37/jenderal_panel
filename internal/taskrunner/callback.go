package taskrunner

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// RunFunc keeps multi-step service work visible through the same task API as
// command tasks. Its lifetime is independent of the initiating HTTP request.
func (r *Runner) RunFunc(name string, work func(context.Context, func(string)) error) string {
	return r.RunFuncWithOptions(Options{Name: name, Timeout: 30 * time.Minute}, work)
}

func (r *Runner) RunFuncWithOptions(options Options, work func(context.Context, func(string)) error) string {
	if options.Timeout <= 0 {
		options.Timeout = 30 * time.Minute
	}
	task := r.startTask(options)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
		defer cancel()
		var err error
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("background task panicked: %v", recovered)
			}
			r.finishTask(task.ID, err)
		}()
		log := func(output string) {
			if output == "" {
				return
			}
			if !strings.HasSuffix(output, "\n") {
				output += "\n"
			}
			r.appendOutput(task.ID, output)
		}
		err = work(ctx, log)
		if err == nil {
			err = ctx.Err()
		}
	}()
	return task.ID
}
