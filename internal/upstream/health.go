package upstream

type ResultKind uint8

const (
	ResultSuccess ResultKind = iota
	ResultAvailabilityFailure
	ResultBusinessError
)

func (pool *Pool) SetHealthy(endpointID string, healthy bool) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if endpoint := pool.byID[endpointID]; endpoint != nil {
		endpoint.healthy = healthy
		if !healthy {
			for affinity, selected := range pool.affinity {
				if selected == endpointID {
					delete(pool.affinity, affinity)
				}
			}
		}
	}
}

func (pool *Pool) QuarantineReason(endpointID string) (string, bool) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	endpoint := pool.byID[endpointID]
	if endpoint == nil || endpoint.quarantine == "" {
		return "", false
	}
	return endpoint.quarantine, true
}
