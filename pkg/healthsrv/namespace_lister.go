package healthsrv

import (
	v1 "k8s.io/client-go/pkg/api/v1"
)

// NamespaceLister is an (*k8s.io/client-go/pkg/api/v1).NamespaceLister compatible interface
// that provides just the ListBuckets cross-section of functionality. It can also be implemented
// for unit tests.
type NamespaceLister interface {
	// List lists all namespaces that are selected by the given label and field selectors.
	List(opts v1.ListOptions) (*v1.NamespaceList, error)
}

// listNamespaces calls nl.List(...) and sends the results back on the various given channels.
// This func is intended to be run in a goroutine and communicates via the channels it's passed.
//
// On success, it passes the namespace list on succCh, and on failure, it passes the error on
// errCh. At most one of {succCh, errCh} will be sent on. If stopCh is closed, no pending or
// future sends will occur.
func listNamespaces(nl NamespaceLister, succCh chan<- *v1.NamespaceList, errCh chan<- error, stopCh <-chan struct{}) {
	nsList, err := nl.List(v1.ListOptions{})
	if err != nil {
		select {
		case errCh <- err:
		case <-stopCh:
		}
		return
	}
	select {
	case succCh <- nsList:
	case <-stopCh:
		return
	}
}
