// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package core

func sliceHasValue(needle string, haystack []string) bool {
	for _, straw := range haystack {
		if straw == needle {
			return true
		}
	}

	return false
}
