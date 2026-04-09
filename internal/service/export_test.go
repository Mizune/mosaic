package service

import "context"

// DeliverOnceForTest exposes the package-private delivery tick to
// tests in the service_test package so they can step the loop
// deterministically instead of depending on the ticker.
func DeliverOnceForTest(ctx context.Context, m *MessagingService) error {
	return m.deliverOnce(ctx)
}
