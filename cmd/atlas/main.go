package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/udbhavbalaji/Atlas/internal/kernel/catalog"
	"github.com/udbhavbalaji/Atlas/internal/kernel/eventbus"
	"github.com/udbhavbalaji/Atlas/internal/kernel/events"
	"github.com/udbhavbalaji/Atlas/internal/kernel/eventstore"
)

const defaultEventStorePath = ".atlas/events/events.jsonl"

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usage(stderr)
	}

	switch args[0] {
	case "events":
		return runEvents(ctx, args[1:], stdout, stderr)
	default:
		return usage(stderr)
	}
}

func runEvents(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return eventsUsage(stderr)
	}

	switch args[0] {
	case "publish":
		return publishEvent(ctx, args[1:], stdout)
	case "replay":
		return replayEvents(ctx, args[1:], stdout)
	default:
		return eventsUsage(stderr)
	}
}

func publishEvent(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("events publish", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var eventType string
	var source string
	var actor string
	var sensitivity string
	var payloadJSON string
	var storePath string
	flags.StringVar(&eventType, "type", "", "canonical event type")
	flags.StringVar(&source, "source", "", "stable source component")
	flags.StringVar(&actor, "actor", "", "event actor")
	flags.StringVar(&sensitivity, "sensitivity", "", "event sensitivity")
	flags.StringVar(&payloadJSON, "payload-json", "{}", "JSON event payload")
	flags.StringVar(&storePath, "store", defaultEventStorePath, "event store path")
	if err := flags.Parse(args); err != nil {
		return err
	}

	var payload events.Payload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return fmt.Errorf("parse payload-json: %w", err)
	}
	if payload == nil {
		payload = events.Payload{}
	}

	event, err := events.New(events.EventType(eventType), source, events.Actor(actor), events.Sensitivity(sensitivity), payload)
	if err != nil {
		return err
	}

	registry := catalog.NewRegistry()
	store := eventstore.NewJSONLStore(filepath.Clean(storePath))
	bus, err := eventbus.New(store, func(event events.Event) error {
		return events.Validate(event, registry)
	})
	if err != nil {
		return err
	}

	if err := bus.Publish(ctx, event); err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, string(encoded))
	return nil
}

func replayEvents(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("events replay", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var storePath string
	flags.StringVar(&storePath, "store", defaultEventStorePath, "event store path")
	if err := flags.Parse(args); err != nil {
		return err
	}

	registry := catalog.NewRegistry()
	store := eventstore.NewJSONLStore(filepath.Clean(storePath))
	bus, err := eventbus.New(store, func(event events.Event) error {
		return events.Validate(event, registry)
	})
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(stdout)
	return bus.Replay(ctx, func(ctx context.Context, event events.Event) error {
		return encoder.Encode(event)
	})
}

func usage(w io.Writer) error {
	fmt.Fprintln(w, "usage: atlas events <publish|replay>")
	return errors.New("unknown command")
}

func eventsUsage(w io.Writer) error {
	fmt.Fprintln(w, "usage: atlas events <publish|replay>")
	return errors.New("unknown events command")
}
