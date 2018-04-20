/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"k8s.io/test-infra/testgrid/util/gcs"

	"cloud.google.com/go/storage"
)

type multiString []string

func (m multiString) String() string {
	return strings.Join(m, ",")
}

func (m *multiString) Set(v string) error {
	*m = strings.Split(v, ",")
	return nil
}

type options struct {
	creds     string
	inputs    multiString
	oneshot   bool
	output    string
	printText bool
}

func gatherOptions() options {
	o := options{}
	flag.StringVar(&o.creds, "gcp-service-account", "", "/path/to/gcp/creds (use local creds if empty")
	flag.BoolVar(&o.oneshot, "oneshot", false, "Write proto once and exit instead of monitoring --yaml files for changes")
	flag.StringVar(&o.output, "output", "", "write proto to gs://bucket/obj or /local/path")
	flag.BoolVar(&o.printText, "print-text", false, "print generated proto in text format to stdout")
	flag.Var(&o.inputs, "yaml", "comma-separated list of input YAML files")
	flag.Parse()
	return o
}

func (o *options) validate() error {
	if len(o.inputs) == 0 || o.inputs[0] == "" {
		return errors.New("--yaml must include at least one file")
	}

	if !o.printText && o.output == "" {
		return errors.New("--print-text or --output=gs://path required")
	}
	return nil
}

// announceChanges watches for changes to files and writes one of them to the channel
func announceChanges(ctx context.Context, paths []string, channel chan []string) {
	defer close(channel)
	modified := map[string]time.Time{}
	for _, p := range paths {
		modified[p] = time.Time{} // Never seen
	}

	// TODO(fejta): consider waiting for a notification rather than polling
	// but performance isn't that big a deal here.
	for {
		var changed []string
		for p, last := range modified {
			select {
			case <-ctx.Done():
				return
			default:
			}
			switch info, err := os.Stat(p); {
			case os.IsNotExist(err) && !last.IsZero():
				// File deleted
				modified[p] = time.Time{}
				changed = append(changed, p)
			case err != nil:
				log.Printf("Error reading %s: %v", p, err)
			default:
				if t := info.ModTime(); t.After(last) {
					changed = append(changed, p)
					modified[p] = t
				}
			}
		}
		if len(changed) > 0 {
			select {
			case <-ctx.Done():
				return
			case channel <- changed:
			}
		} else {
			time.Sleep(1 * time.Second)
		}
	}
}

func readConfig(paths []string) (*Config, error) {
	var c Config
	for _, file := range paths {
		b, err := ioutil.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %v", file, err)
		}
		if err = c.Update(b); err != nil {
			return nil, fmt.Errorf("failed to merge %s into config: %v", file, err)
		}
	}
	return &c, nil
}

func write(client *storage.Client, ctx context.Context, path string, bytes []byte) error {
	u, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("invalid url %s: %v", path, err)
	}
	if u.Scheme != "gs" {
		return ioutil.WriteFile(path, bytes, 0644)
	}
	var p gcs.Path
	if err = p.SetURL(u); err != nil {
		return err
	}
	return gcs.Upload(client, ctx, p, bytes)
}

func doOneshot(client *storage.Client, ctx context.Context, opt options) error {
	// Ignore what changed for now and just recompute everything
	c, err := readConfig(opt.inputs)
	if err != nil {
		return fmt.Errorf("could not read config: %v", err)
	}

	// Print proto if requested
	if opt.printText {
		if err := c.MarshalText(os.Stdout); err != nil {
			return fmt.Errorf("could not print config: %v", err)
		}
	}

	// Write proto if requested
	if opt.output != "" {
		b, err := c.MarshalBytes()
		if err == nil {
			err = write(client, ctx, opt.output, b)
		}
		if err != nil {
			return fmt.Errorf("could not write config: %v", err)
		}
	}
	return nil
}

func main() {
	// Parse flags
	opt := gatherOptions()
	if err := opt.validate(); err != nil {
		log.Fatalf("Bad flags: %v", err)
	}

	// Setup stuff
	ctx := context.Background()
	client, err := gcs.ClientWithCreds(ctx, opt.creds)
	if err != nil {
		log.Fatalf("Failed to create storage client: %v", err)
	}

	// Oneshot mode, write config and exit
	if opt.oneshot {
		if err := doOneshot(client, ctx, opt); err != nil {
			log.Fatalf("FAIL: %v", err)
		}
		return
	}

	// Service mode, monitor input files for changes
	channel := make(chan []string)
	// Monitor files for changes
	go announceChanges(ctx, opt.inputs, channel)

	// Wait for changed files
	for changes := range channel {
		log.Printf("Changed: %v", changes)
		log.Println("Writing config...")
		if err := doOneshot(client, ctx, opt); err != nil {
			log.Printf("FAIL: %v", err)
			continue
		}
		log.Printf("Wrote config to %s", opt.output)
	}
}
