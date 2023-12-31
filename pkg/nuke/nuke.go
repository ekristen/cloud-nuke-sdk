// Package nuke provides a framework for removing resources by providing commands to register resource scanners,
// validation handlers, resource types and filters.
package nuke

import (
	"fmt"
	"github.com/ekristen/libnuke/pkg/featureflag"
	"github.com/ekristen/libnuke/pkg/filter"
	"github.com/ekristen/libnuke/pkg/queue"
	"github.com/ekristen/libnuke/pkg/resource"
	"github.com/ekristen/libnuke/pkg/types"
	"github.com/ekristen/libnuke/pkg/utils"
	"github.com/sirupsen/logrus"
	"time"
)

// ListCache is used to cache the list of resources that are returned from the API.
type ListCache map[string]map[string][]resource.Resource

// Parameters is a collection of common variables used to configure the before of the Nuke instance.
type Parameters struct {
	ConfigPath string

	NoDryRun   bool
	Force      bool
	ForceSleep int
	Quiet      bool

	MaxWaitRetries int
}

type INuke interface {
	Run() error
	Scan() error
	Filter(item *queue.Item) error
	HandleQueue()
	HandleRemove(item *queue.Item)
	HandleWait(item *queue.Item, cache ListCache)
}

// Nuke is the main struct for the library. It is used to register resource types, scanners, filters and validation
// handlers.
type Nuke struct {
	Parameters   Parameters
	Queue        queue.Queue
	Filters      filter.Filters
	FeatureFlags *featureflag.FeatureFlags

	ValidateHandlers []func() error

	ResourceTypes map[resource.Scope]types.Collection
	Scanners      map[resource.Scope][]*Scanner

	prompts map[string]func() error

	version string
}

// RegisterVersion allows the tool instantiating the library to register its version so there's consist output
// of the version information across all tools. It is optional.
func (n *Nuke) RegisterVersion(version string) {
	n.version = version
}

// RegisterFeatureFlags allows the tool instantiating the library to register a boolean flag. For example, aws nuke
// needs to be able to register if disabling of instance deletion protection is allowed, this provides a generic method
// for doing that.
func (n *Nuke) RegisterFeatureFlags(flag string, defaultValue *bool, value *bool) {
	n.FeatureFlags.New(flag, defaultValue, value)
}

// RegisterValidateHandler allows the tool instantiating the library to register a validation handler. It is optional.
func (n *Nuke) RegisterValidateHandler(handler func() error) {
	if n.ValidateHandlers == nil {
		n.ValidateHandlers = make([]func() error, 0)
	}

	n.ValidateHandlers = append(n.ValidateHandlers, handler)
}

// RegisterResourceTypes is used to register resource types against a scope. A scope is a string that is used to
// group resource types together. For example, you could have a scope of "aws" and register all AWS resource types.
// For Azure, you have to register resources by tenant or subscription or even resource group.
func (n *Nuke) RegisterResourceTypes(scope resource.Scope, resourceTypes ...string) {
	if n.ResourceTypes == nil {
		n.ResourceTypes = make(map[resource.Scope]types.Collection)
	}

	n.ResourceTypes[scope] = append(n.ResourceTypes[scope], resourceTypes...)
}

// RegisterScanner is used to register a scanner against a scope. A scope is a string that is used to group resource
// types together. A scanner is what is responsible for actually querying the API for resources and adding them to
// the queue for processing.
func (n *Nuke) RegisterScanner(scope resource.Scope, scanner *Scanner) {
	if n.Scanners == nil {
		n.Scanners = make(map[resource.Scope][]*Scanner)
	}

	// TODO: register them by hashing the scanner object to detect duplicates

	n.Scanners[scope] = append(n.Scanners[scope], scanner)
}

func (n *Nuke) RegisterPrompt(name string, prompt func() error) {
	if n.prompts == nil {
		n.prompts = make(map[string]func() error)
	}

	n.prompts[name] = prompt
}

func (n *Nuke) PromptFirst() error {
	if prompt, ok := n.prompts["first"]; ok {
		return prompt()
	}

	return nil
}

func (n *Nuke) PromptSecond() error {
	if prompt, ok := n.prompts["second"]; ok {
		return prompt()
	}

	return nil
}

// Run is the main entry point for the library. It will run the validation handlers, prompt the user, scan for
// resources, filter them and then process them.
func (n *Nuke) Run() error {
	if err := n.Validate(); err != nil {
		return err
	}

	if err := n.PromptFirst(); err != nil {
		return err
	}

	if err := n.Scan(); err != nil {
		return err
	}

	if n.Queue.Count(queue.ItemStateNew) == 0 {
		fmt.Println("No resource to delete.")
		return nil
	}

	if !n.Parameters.NoDryRun {
		fmt.Println("The above resources would be deleted with the supplied configuration. Provide --no-dry-run to actually destroy resources.")
		return nil
	}

	if err := n.PromptSecond(); err != nil {
		return err
	}

	failCount := 0
	waitingCount := 0

	for {
		n.HandleQueue()

		if n.Queue.Count(queue.ItemStatePending, queue.ItemStateWaiting, queue.ItemStateNew, queue.ItemStateNewDependency) == 0 && n.Queue.Count(queue.ItemStateFailed) > 0 {
			if failCount >= 2 {
				logrus.Errorf("There are resources in failed state, but none are ready for deletion, anymore.")
				fmt.Println()

				for _, item := range n.Queue.GetItems() {
					if item.GetState() != queue.ItemStateFailed {
						continue
					}

					item.Print()
					logrus.Error(item.GetReason())
				}

				return fmt.Errorf("failed")
			}

			failCount = failCount + 1
		} else {
			failCount = 0
		}
		if n.Parameters.MaxWaitRetries != 0 && n.Queue.Count(queue.ItemStateWaiting, queue.ItemStatePending) > 0 && n.Queue.Count(queue.ItemStateNew, queue.ItemStateNewDependency) == 0 {
			if waitingCount >= n.Parameters.MaxWaitRetries {
				return fmt.Errorf("Max wait retries of %d exceeded.\n\n", n.Parameters.MaxWaitRetries)
			}
			waitingCount = waitingCount + 1
		} else {
			waitingCount = 0
		}
		if n.Queue.Count(queue.ItemStateNew, queue.ItemStateNewDependency, queue.ItemStatePending, queue.ItemStateFailed, queue.ItemStateWaiting) == 0 {
			break
		}

		time.Sleep(5 * time.Second)
	}

	fmt.Printf("Nuke complete: %d failed, %d skipped, %d finished.\n\n",
		n.Queue.Count(queue.ItemStateFailed), n.Queue.Count(queue.ItemStateFiltered), n.Queue.Count(queue.ItemStateFinished))

	return nil
}

// Version prints the version that was registered with the library by the invoking tool.
func (n *Nuke) Version() {
	fmt.Println(n.version)
}

// Validate is used to run the validation handlers that were registered with the library by the invoking tool.
func (n *Nuke) Validate() error {
	if n.Parameters.ForceSleep < 3 {
		return fmt.Errorf("value for --force-sleep cannot be less than 3 seconds. This is for your own protection")
	}

	n.Version()

	for _, handler := range n.ValidateHandlers {
		if err := handler(); err != nil {
			return err
		}
	}

	return nil
}

// Scan is used to scan for resources. It will run the scanners that were registered with the library by the invoking
// tool. It will also filter the resources based on the filters that were registered. It will also print the current
// status of the resources.
func (n *Nuke) Scan() error {
	itemQueue := queue.Queue{
		Items: make([]*queue.Item, 0),
	}

	for _, scanners := range n.Scanners {
		for _, scanner := range scanners {
			err := scanner.Run()
			if err != nil {
				return err
			}

			for item := range scanner.Items {
				ffGetter, ok := item.Resource.(resource.FeatureFlagGetter)
				if ok {
					ffGetter.FeatureFlags(n.FeatureFlags)
				}

				itemQueue.Items = append(itemQueue.Items, item)
				err := n.Filter(item)
				if err != nil {
					return err
				}

				if item.State != queue.ItemStateFiltered || !n.Parameters.Quiet {
					item.Print()
				}
			}
		}
	}

	fmt.Printf("Scan complete: %d total, %d nukeable, %d filtered.\n\n",
		itemQueue.Total(), itemQueue.Count(queue.ItemStateNew, queue.ItemStateNewDependency), itemQueue.Count(queue.ItemStateFiltered))

	n.Queue = itemQueue

	return nil
}

// Filter is used to filter resources. It will run the filters that were registered with the instance of Nuke
// and set the state of the resource to filtered if it matches the filter.
func (n *Nuke) Filter(item *queue.Item) error {
	checker, ok := item.Resource.(resource.Filter)
	if ok {
		err := checker.Filter()
		if err != nil {
			item.State = queue.ItemStateFiltered
			item.Reason = err.Error()

			// Not returning the error, since it could be because of a failed
			// request to the API. We do not want to block the whole nuking,
			// because of an issue on AWS side.
			return nil
		}
	}

	accountFilters := n.Filters

	itemFilters, ok := accountFilters[item.Type]
	if !ok {
		return nil
	}

	for _, f := range itemFilters {
		prop, err := item.GetProperty(f.Property)
		if err != nil {
			return err
		}

		match, err := f.Match(prop)
		if err != nil {
			return err
		}

		if utils.IsTrue(f.Invert) {
			match = !match
		}

		if match {
			item.State = queue.ItemStateFiltered
			item.Reason = "filtered by config"
			return nil
		}
	}

	return nil
}

// HandleQueue is used to handle the queue of resources. It will iterate over the queue and trigger the appropriate
// handlers based on the state of the resource.
func (n *Nuke) HandleQueue() {
	listCache := make(map[string]map[string][]resource.Resource)

	for _, item := range n.Queue.GetItems() {
		switch item.GetState() {
		case queue.ItemStateNew:
			n.HandleRemove(item)
			item.Print()
		case queue.ItemStateNewDependency:
			n.HandleWaitDependency(item)
			item.Print()
		case queue.ItemStateFailed:
			n.HandleRemove(item)
			n.HandleWait(item, listCache)
			item.Print()
		case queue.ItemStatePending:
			n.HandleWait(item, listCache)
			item.State = queue.ItemStateWaiting
			item.Print()
		case queue.ItemStateWaiting:
			n.HandleWait(item, listCache)
			item.Print()
		}

	}

	fmt.Println()
	fmt.Printf("Removal requested: %d waiting, %d failed, %d skipped, %d finished\n\n",
		n.Queue.Count(queue.ItemStateWaiting, queue.ItemStatePending, queue.ItemStateNewDependency), n.Queue.Count(queue.ItemStateFailed),
		n.Queue.Count(queue.ItemStateFiltered), n.Queue.Count(queue.ItemStateFinished))
}

// HandleRemove is used to handle the removal of a resource. It will remove the resource and set the state of the
// resource to pending if it was successful or failed if it was not.
func (n *Nuke) HandleRemove(item *queue.Item) {
	err := item.Resource.Remove()
	if err != nil {
		item.State = queue.ItemStateFailed
		item.Reason = err.Error()
		return
	}

	item.State = queue.ItemStatePending
	item.Reason = ""
}

// HandleWaitDependency is used to handle the waiting of a resource. It will check if the resource has any dependencies
// and if it does, it will check if the dependencies have been removed. If they have, it will trigger the remove handler.
func (n *Nuke) HandleWaitDependency(item *queue.Item) {
	reg := resource.GetRegistration(item.Type)
	depCount := 0
	for _, dep := range reg.DependsOn {
		cnt := n.Queue.CountByType(dep, queue.ItemStateNew, queue.ItemStatePending, queue.ItemStateWaiting)
		depCount = depCount + cnt
	}

	if depCount == 0 {
		n.HandleRemove(item)
	}
}

// HandleWait is used to handle the waiting of a resource. It will check if the resource has been removed. If it has,
// it will set the state of the resource to finished. If it has not, it will set the state of the resource to waiting.
func (n *Nuke) HandleWait(item *queue.Item, cache ListCache) {
	var err error
	ownerId := item.Owner
	_, ok := cache[ownerId]
	if !ok {
		cache[ownerId] = make(map[string][]resource.Resource)
	}
	left, ok := cache[ownerId][item.Type]
	if !ok {
		left, err = item.List(item.Opts)
		if err != nil {
			item.State = queue.ItemStateFailed
			item.Reason = err.Error()
			return
		}
		cache[ownerId][item.Type] = left
	}

	for _, r := range left {
		if item.Equals(r) {
			checker, ok := r.(resource.Filter)
			if ok {
				err := checker.Filter()
				if err != nil {
					break
				}
			}

			return
		}
	}

	item.State = queue.ItemStateFinished
	item.Reason = ""
}
