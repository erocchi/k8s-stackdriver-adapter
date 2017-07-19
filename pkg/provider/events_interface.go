package provider


type EventsProvider interface {
	GetNamespacedEventsByName(namespace, eventName string) (EventValue, error)
}


type Provider struct {

}



