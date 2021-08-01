package fscmds

/*
type keyFunc func(manager.Request) (indexKey, error)

// TODO: docs; inspects, passes or errors
func checkConstraints(ctx context.Context, sections sectionStream) (sectionStream, errorStream) {
	requests, errors := make(sectionStreamRW), make(errorStreamRW)
	panic("niy")
	return requests, errors
	//var wg sync.WaitGroup

					// [legacy - deviation] The existing implementation implicitly tries to unmount targets
				// if they are already mounted. If we encounter this case we return an error for that target.
				if err == nil {
					if existing, ok := dupeCheck[key]; ok {
						err = fmt.Errorf("target %q already requested by %v", key, existing)
					}
				}
				if err != nil {
					select {
					case <-ctx.Done():
					case errors <- err:
					}
					return
				}
			}
		}()
}

func checkSection(ctx context.Context, section *requestSection) (sectionStream, errorStream) {
	var (
		validOrDie = make(sectionStreamRW)
		errors     = make(errorStreamRW)
	)

	go func() {
		defer close(errors)
		response = &requestSection{requestHeader: section.requestHeader}

		var (
			keys        <-chan indexKey
			subrequests requestStream
			suberrors   errors
		)

		switch section.API {
		default: // unknown
			select {
			case <-ctx.Done():
			case errors <- fmt.Errorf("unexpected api %v", section.API): // TODO: errmsg
			}
			return
		// subs:
		case filesystem.Fuse:
			keys, subrequests, suberrors = checkFuseSection(ctx, section.requests)
			// TODO: send to checker here
			// ^ that is, some top level listener (rcv <-channel)
			// which relays the requests, and shares a map across all APIS
			// also handles sync access to the map via top level goroutine's/listener's rcv
		}

		if suberrors != nil {
			select {
			case <-ctx.Done():
				return
			case suberrors <- suberrors:
			}
		}

		select {
		case <-ctx.Done():
			return
		case requests <- soureOrSub: // check passed, let it through
		}
	}()

	return validOrDie, errors
}

func requestsToIndexKeys(ctx context.Context, hasher keyFunc, requests requestStream) (<-chan indexKey, errorStream) {
	out := make(chan indexKey, len(requests))
	errors := make(errorStreamRW)
	go func() {
		for request := range requests {
			key, err := hasher(request)
			if err != nil {
				select {
				case <-ctx.Done():
					return
				case errors <- err:
				}
			}
			out <- key
		}
	}()
	return out, errors
}

// TODO: split this up; return keyFunc(req) (key, error)
func checkFuseSection(ctx context.Context, requests requestStream) (<-chan indexKey, requestStream, errorStream) {
	index := make(chan indexKey)
	fuseRequests := make(requestStreamRW)
	errors := make(errorStreamRW)

	go func() {
		defer close(index)
		defer close(fuseRequests)
		defer close(errors)
		for request := range requests {
			key, err = multiaddr.Cast(request).ValueForProtocol(int(filesystem.PathProtocol))
		}
	}()

	relay := make(requestStreamRW)
	errors := make(errorStreamRW)
	go func() {
		defer close(relay)
		defer close(errors)
		for request := range requests {
			var requestMountpoint string
			var err error
			if request != nil { // TODO: hax this nicer
				requestMountpoint, err = multiaddr.Cast(request).ValueForProtocol(int(filesystem.PathProtocol))
			} else {
				err = multiaddr.ErrProtocolNotFound
			}
			switch err {
			case nil: // request has expected values, do nothing
			case multiaddr.ErrProtocolNotFound: // request is missing a target value
				switch nodeAPI { // supply one from the config's value
				case filesystem.IPFS:
					requestMountpoint = nodeConf.Mounts.IPFS
				case filesystem.IPNS:
					requestMountpoint = nodeConf.Mounts.IPNS
				default:
					err = fmt.Errorf("protocol %v has no config value", nodeAPI)
					return
				}
				var configComponent *multiaddr.Component
				if configComponent, err = multiaddr.NewComponent(filesystem.PathProtocol.String(), requestMountpoint); err != nil {
					return
				}
				request = configComponent.Bytes()
			}
			if err != nil {
				select {
				case <-ctx.Done():
				case errors <- err:
				}
				return
			}
			select {
			case <-ctx.Done():
				return
			case relay <- request:
			}
		}
	}()
	return relay, errors

}
*/
