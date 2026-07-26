// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package palworld_test

import (
	"fmt"
	"log"

	"github.com/LukeHollandDev/palworld-save-reader/internal/gvas"
	"github.com/LukeHollandDev/palworld-save-reader/internal/palworld"
)

func ExampleLoad() {
	save, err := palworld.Load("LevelMeta.sav")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(save.Header.ClassName)
	for _, property := range save.Properties {
		fmt.Printf("%s (%s): %#v\n", property.Name, property.Type, property.Value)
	}
}

// ExampleLoad_iterate reads a large struct array one element at a time. The
// eager Values method would hold all 9,600 decoded parameter sets at once.
func ExampleLoad_iterate() {
	save, err := palworld.Load("player_dps.sav")
	if err != nil {
		log.Fatal(err)
	}

	property := save.Properties.Find("SaveParameterArray")
	array := property.Value.(gvas.ArrayValue).Structs
	iterator := array.Iterator()
	for iterator.Next() {
		properties := iterator.Value().(gvas.Properties)
		_ = properties
	}
	if err := iterator.Err(); err != nil {
		log.Fatal(err)
	}
}
