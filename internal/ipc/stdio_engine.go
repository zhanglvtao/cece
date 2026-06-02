package ipc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sync"

	"cece/internal/protocol"
)

type Runtime interface {
	Input(context.Context, string) error
	Do(protocol.Action)
	Events() <-chan protocol.Event
	Wait() // block until background tasks complete (for graceful shutdown)
}

func Serve(ctx context.Context, runtime Runtime, stdin io.Reader, stdout io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var writeMu sync.Mutex
	writeLine := func(line []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err := stdout.Write(append(line, '\n'))
		return err
	}

	var wg sync.WaitGroup
	events := runtime.Events()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case ev, ok := <-events:
				if !ok {
					return
				}
				line, err := MarshalEvent(ev)
				if err != nil {
					line, _ = MarshalError(err.Error())
				}
				_ = writeLine(line)
			default:
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-events:
					if !ok {
						return
					}
					line, err := MarshalEvent(ev)
					if err != nil {
						line, _ = MarshalError(err.Error())
					}
					_ = writeLine(line)
				}
			}
		}
	}()

	scanner := bufio.NewScanner(stdin)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			wg.Wait()
			runtime.Wait()
			return nil
		default:
		}
		msg, err := UnmarshalClientMessage(scanner.Bytes())
		if err != nil {
			line, _ := MarshalError(err.Error())
			_ = writeLine(line)
			continue
		}
		if input, ok := msg.Action.(protocol.InputAction); ok {
			if err := runtime.Input(ctx, input.Text); err != nil {
				line, _ := MarshalError(err.Error())
				_ = writeLine(line)
			}
			continue
		}
		runtime.Do(msg.Action)
	}
	if err := scanner.Err(); err != nil {
		cancel()
		wg.Wait()
		runtime.Wait()
		return fmt.Errorf("read stdin: %w", err)
	}
	cancel()
	wg.Wait()
	runtime.Wait()
	return nil
}
