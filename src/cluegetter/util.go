// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

func sliceHasValue(needle string, haystack []string) bool {
	for _, straw := range haystack {
		if straw == needle {
			return true
		}
	}

	return false
}
