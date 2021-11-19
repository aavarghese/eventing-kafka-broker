package v1alpha1

import (
	eventing "knative.dev/eventing/pkg/apis/eventing/v1"
)

// TODO Add this type in Eventing core

// Filter is the filter to apply against all events. Only events that pass this
// filter will be sent to the Subscriber.
// If not specified, will default to allowing all events.
type Filter struct {

	// Filters is an experimental field that conforms to the CNCF CloudEvents Subscriptions
	// API. It's an array of filter expressions that evaluate to true or false.
	// If any filter expression in the array evaluates to false, the event MUST
	// NOT be sent to the Subscriber. If all the filter expressions in the array
	// evaluate to true, the event MUST be attempted to be delivered. Absence of
	// a filter or empty array implies a value of true. In the event of users
	// specifying both Filter and Filters, then the latter will override the former.
	// This will allow users to try out the effect of the new Filters field
	// without compromising the existing attribute-based Filter and try it out on existing
	// Trigger objects.
	//
	// +optional
	Filters []eventing.SubscriptionsAPIFilter `json:"filters,omitempty"`
}
