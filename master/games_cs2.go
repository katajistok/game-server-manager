package main

func init() {
	RegisterGame(GameDefinition{
		Name:        "Counter-Strike 2",
		Slug:        "cs2",
		DockerImage: "cm2network/cs2",
		DefaultPort: 27015,
		MaxPlayers:  10,
		DataPath:    "/home/steam/cs2-dedicated",
		DefaultEnv: map[string]string{
			"CS2_IP":              "0.0.0.0",
			"CS2_LAN":             "1",
			"CS2_ADDITIONAL_ARGS": "-usercon",
			"TV_ENABLE":           "1",
		},
		FieldMappings: GameFieldMappings{
			EnvMaxPlayers:   "CS2_MAXPLAYERS",
			EnvPassword:     "CS2_PW",
			EnvRconPassword: "CS2_RCONPW",
			RconPort:        27015,
			PortDerivedVars: map[string]int{"TV_PORT": 5},
		},
		CustomFields: []GameFieldDef{
			{Key: "CS2_STARTMAP", Label: "Start Map", Placeholder: "de_dust2", Type: "text"},
			{Key: "CS2_MAPGROUP", Label: "Map Group", Placeholder: "mg_active", Type: "text"},
			{Key: "CS2_ADDITIONAL_ARGS", Label: "Extra Args", Placeholder: "+sv_cheats 1", Type: "text"},
		},
		PortMappings: []PortMappingDef{
			{Label: "game", ContainerPort: 27015, Protocol: "both", HostPortOffset: 0, Description: "CS2 game traffic"},
			{Label: "tv", ContainerPort: 27020, Protocol: "udp", HostPortOffset: 5, Description: "SourceTV"},
		},
		Modes: []GameModeDef{
			{Name: "Competitive", Slug: "competitive", Config: map[string]string{
				"CS2_GAMEALIAS": "competitive", "CS2_STARTMAP": "de_dust2",
			}},
			{Name: "Deathmatch", Slug: "deathmatch", Config: map[string]string{
				"CS2_GAMEALIAS": "deathmatch", "CS2_STARTMAP": "de_mirage",
			}},
			{Name: "Casual", Slug: "casual", Config: map[string]string{
				"CS2_GAMEALIAS": "casual", "CS2_STARTMAP": "de_dust2",
			}},
		},
	})
}
