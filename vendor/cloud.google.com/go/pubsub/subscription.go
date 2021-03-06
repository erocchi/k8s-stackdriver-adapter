// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pubsub

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/iam"
	"golang.org/x/net/context"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// Subscription is a reference to a PubSub subscription.
type Subscription struct {
	s service

	// The fully qualified identifier for the subscription, in the format "projects/<projid>/subscriptions/<name>"
	name string

	// Settings for pulling messages. Configure these before calling Receive.
	ReceiveSettings ReceiveSettings

	mu            sync.Mutex
	receiveActive bool
}

// Subscription creates a reference to a subscription.
func (c *Client) Subscription(id string) *Subscription {
	return newSubscription(c.s, fmt.Sprintf("projects/%s/subscriptions/%s", c.projectID, id))
}

func newSubscription(s service, name string) *Subscription {
	return &Subscription{
		s:    s,
		name: name,
	}
}

// String returns the globally unique printable name of the subscription.
func (s *Subscription) String() string {
	return s.name
}

// ID returns the unique identifier of the subscription within its project.
func (s *Subscription) ID() string {
	slash := strings.LastIndex(s.name, "/")
	if slash == -1 {
		// name is not a fully-qualified name.
		panic("bad subscription name")
	}
	return s.name[slash+1:]
}

// Subscriptions returns an iterator which returns all of the subscriptions for the client's project.
func (c *Client) Subscriptions(ctx context.Context) *SubscriptionIterator {
	return &SubscriptionIterator{
		s:    c.s,
		next: c.s.listProjectSubscriptions(ctx, c.fullyQualifiedProjectName()),
	}
}

// SubscriptionIterator is an iterator that returns a series of subscriptions.
type SubscriptionIterator struct {
	s    service
	next nextStringFunc
}

// Next returns the next subscription. If there are no more subscriptions, iterator.Done will be returned.
func (subs *SubscriptionIterator) Next() (*Subscription, error) {
	subName, err := subs.next()
	if err != nil {
		return nil, err
	}
	return newSubscription(subs.s, subName), nil
}

// PushConfig contains configuration for subscriptions that operate in push mode.
type PushConfig struct {
	// A URL locating the endpoint to which messages should be pushed.
	Endpoint string

	// Endpoint configuration attributes. See https://cloud.google.com/pubsub/docs/reference/rest/v1/projects.subscriptions#pushconfig for more details.
	Attributes map[string]string
}

// Subscription config contains the configuration of a subscription.
type SubscriptionConfig struct {
	Topic      *Topic
	PushConfig PushConfig

	// The default maximum time after a subscriber receives a message before
	// the subscriber should acknowledge the message. Note: messages which are
	// obtained via Subscription.Receive need not be acknowledged within this
	// deadline, as the deadline will be automatically extended.
	AckDeadline time.Duration
}

// ReceiveSettings configure the Receive method.
// A zero ReceiveSettings will result in values equivalent to DefaultReceiveSettings.
type ReceiveSettings struct {
	// MaxExtension is the maximum period for which the Subscription should
	// automatically extend the ack deadline for each message.
	//
	// The Subscription will automatically extend the ack deadline of all
	// fetched Messages for the duration specified. Automatic deadline
	// extension may be disabled by specifying a duration less than 1.
	MaxExtension time.Duration

	// MaxOutstandingMessages is the maximum number of unprocessed messages
	// (unacknowledged but not yet expired). If MaxOutstandingMessages is 0, it
	// will be treated as if it were DefaultReceiveSettings.MaxOutstandingMessages.
	// If the value is negative, then there will be no limit on the number of
	// unprocessed messages.
	MaxOutstandingMessages int

	// MaxOutstandingBytes is the maximum size of unprocessed messages
	// (unacknowledged but not yet expired). If MaxOutstandingBytes is 0, it will
	// be treated as if it were DefaultReceiveSettings.MaxOutstandingBytes. If
	// the value is negative, then there will be no limit on the number of bytes
	// for unprocessed messages.
	MaxOutstandingBytes int
}

// DefaultReceiveSettings holds the default values for ReceiveSettings.
var DefaultReceiveSettings = ReceiveSettings{
	MaxExtension:           10 * time.Minute,
	MaxOutstandingMessages: 1000,
	MaxOutstandingBytes:    1e9, // 1G
}

// Delete deletes the subscription.
func (s *Subscription) Delete(ctx context.Context) error {
	return s.s.deleteSubscription(ctx, s.name)
}

// Exists reports whether the subscription exists on the server.
func (s *Subscription) Exists(ctx context.Context) (bool, error) {
	return s.s.subscriptionExists(ctx, s.name)
}

// Config fetches the current configuration for the subscription.
func (s *Subscription) Config(ctx context.Context) (*SubscriptionConfig, error) {
	conf, topicName, err := s.s.getSubscriptionConfig(ctx, s.name)
	if err != nil {
		return nil, err
	}
	conf.Topic = &Topic{
		s:    s.s,
		name: topicName,
	}
	return conf, nil
}

// ModifyPushConfig updates the endpoint URL and other attributes of a push subscription.
func (s *Subscription) ModifyPushConfig(ctx context.Context, conf *PushConfig) error {
	if conf == nil {
		return errors.New("must supply non-nil PushConfig")
	}

	return s.s.modifyPushConfig(ctx, s.name, conf)
}

func (s *Subscription) IAM() *iam.Handle {
	return s.s.iamHandle(s.name)
}

// CreateSubscription creates a new subscription on a topic.
//
// name is the name of the subscription to create. It must start with a letter,
// and contain only letters ([A-Za-z]), numbers ([0-9]), dashes (-),
// underscores (_), periods (.), tildes (~), plus (+) or percent signs (%). It
// must be between 3 and 255 characters in length, and must not start with
// "goog".
//
// topic is the topic from which the subscription should receive messages. It
// need not belong to the same project as the subscription.
//
// ackDeadline is the maximum time after a subscriber receives a message before
// the subscriber should acknowledge the message. It must be between 10 and 600
// seconds (inclusive), and is rounded down to the nearest second. If the
// provided ackDeadline is 0, then the default value of 10 seconds is used.
// Note: messages which are obtained via Subscription.Receive need not be
// acknowledged within this deadline, as the deadline will be automatically
// extended.
//
// pushConfig may be set to configure this subscription for push delivery.
//
// If the subscription already exists an error will be returned.
func (c *Client) CreateSubscription(ctx context.Context, id string, topic *Topic, ackDeadline time.Duration, pushConfig *PushConfig) (*Subscription, error) {
	if ackDeadline == 0 {
		ackDeadline = 10 * time.Second
	}
	if d := ackDeadline.Seconds(); d < 10 || d > 600 {
		return nil, fmt.Errorf("ack deadline must be between 10 and 600 seconds; got: %v", d)
	}

	sub := c.Subscription(id)
	err := c.s.createSubscription(ctx, topic.name, sub.name, ackDeadline, pushConfig)
	return sub, err
}

var errReceiveInProgress = errors.New("pubsub: Receive already in progress for this subscription")

// Receive calls f with the outstanding messages from the subscription.
// It blocks until ctx is done, or the service returns a non-retryable error.
//
// The standard way to terminate a Receive is to cancel its context:
//
//   cctx, cancel := context.WithCancel(ctx)
//   err := sub.Receive(cctx, callback)
//   // Call cancel from callback, or another goroutine.
//
// If the service returns a non-retryable error, Receive returns that error after
// all of the outstanding calls to f have returned. If ctx is done, Receive
// returns either nil after all of the outstanding calls to f have returned and
// all messages have been acknowledged or have expired.
//
// Receive calls f concurrently from multiple goroutines. It is encouraged to
// process messages synchronously in f, even if that processing is relatively
// time-consuming; Receive will spawn new goroutines for incoming messages,
// limited by MaxOutstandingMessages and MaxOutstandingBytes in ReceiveSettings.
//
// The context passed to f will be canceled when ctx is Done or there is a
// fatal service error.
//
// Receive will automatically extend the ack deadline of all fetched Messages for the
// period specified by s.ReceiveSettings.MaxExtension.
//
// Each Subscription may have only one invocation of Receive active at a time.
func (s *Subscription) Receive(ctx context.Context, f func(context.Context, *Message)) error {
	s.mu.Lock()
	if s.receiveActive {
		s.mu.Unlock()
		return errReceiveInProgress
	}
	s.receiveActive = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.receiveActive = false; s.mu.Unlock() }()

	config, err := s.Config(ctx)
	if err != nil {
		if grpc.Code(err) == codes.Canceled {
			return nil
		}
		return err
	}
	maxCount := s.ReceiveSettings.MaxOutstandingMessages
	if maxCount == 0 {
		maxCount = DefaultReceiveSettings.MaxOutstandingMessages
	}
	maxBytes := s.ReceiveSettings.MaxOutstandingBytes
	if maxBytes == 0 {
		maxBytes = DefaultReceiveSettings.MaxOutstandingBytes
	}
	maxExt := s.ReceiveSettings.MaxExtension
	if maxExt == 0 {
		maxExt = DefaultReceiveSettings.MaxExtension
	} else if maxExt < 0 {
		// If MaxExtension is negative, disable automatic extension.
		maxExt = 0
	}
	// TODO(jba): add tests that verify that ReceiveSettings are correctly processed.
	po := &pullOptions{
		maxExtension: maxExt,
		maxPrefetch:  trunc32(int64(maxCount)),
		ackDeadline:  config.AckDeadline,
	}
	fc := newFlowController(maxCount, maxBytes)

	// Wait for all goroutines started by Receive to return, so instead of an
	// obscure goroutine leak we have an obvious blocked call to Receive.
	var wg sync.WaitGroup
	defer wg.Wait()

	return s.receive(ctx, &wg, po, fc, f)
}

func (s *Subscription) receive(ctx context.Context, wg *sync.WaitGroup, po *pullOptions, fc *flowController, f func(context.Context, *Message)) error {
	// Cancel a sub-context when we return, to kick the context-aware callbacks
	// and the goroutine below.
	ctx2, cancel := context.WithCancel(ctx)
	// Call stop when Receive's context is done.
	// Stop will block until all outstanding messages have been acknowledged
	// or there was a fatal service error.
	// The iterator does not use the context passed to Receive. If it did, canceling
	// that context would immediately stop the iterator without waiting for unacked
	// messages.
	iter := newMessageIterator(context.Background(), s.s, s.name, po)
	wg.Add(1)
	go func() {
		<-ctx2.Done()
		iter.Stop()
		wg.Done()
	}()
	defer cancel()
	for {
		msg, err := iter.Next()
		if err == iterator.Done {
			return nil
		}
		if err != nil {
			return err
		}
		// TODO(jba): call acquire closer to when the message is allocated.
		if err := fc.acquire(ctx, len(msg.Data)); err != nil {
			// TODO(jba): test that this "orphaned" message is nacked immediately when ctx is done.
			msg.Nack()
			return nil
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// TODO(jba): call release when the message is available for GC.
			// This considers the message to be released when
			// f is finished, but f may ack early or not at all.
			defer fc.release(len(msg.Data))
			f(ctx2, msg)
		}()
	}
}

// TODO(jba): remove when we delete messageIterator.
type pullOptions struct {
	maxExtension time.Duration
	maxPrefetch  int32
	// ackDeadline is the default ack deadline for the subscription. Not
	// configurable.
	ackDeadline time.Duration
}
