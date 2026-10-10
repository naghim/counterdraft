#!/usr/bin/env python3
"""Regenerates internal/heroes/heroes.json.

Fetches hero metadata (id, name, attribute, attack type, roles) from the
OpenDota hero endpoint and merges it with the hand-authored mechanical tag
table below. No match/winrate statistics are used anywhere.

Usage: python3 tools/gen_heroes.py > internal/heroes/heroes.json
"""
import json
import os
import sys
import urllib.request

CACHE = os.path.join(os.path.dirname(__file__), "heroes_cache.json")

# Mechanical tags, one entry per hero. Keep the vocabulary in sync with the
# rule definitions in internal/rules/rules.go.
TAGS = {
    "Anti-Mage": ["blink", "mobility", "escape", "late_game", "physical", "sustained_dps", "mana_burn", "spell_resist", "carry"],
    "Axe": ["taunt", "aoe", "initiator", "durable", "bkb_pierce", "counter_initiate", "physical"],
    "Bane": ["single_target", "bkb_pierce", "sleep", "fear", "channeled", "disable", "magical"],
    "Bloodseeker": ["gank", "silence", "single_target", "burst", "early_game", "physical"],
    "Crystal Maiden": ["aoe", "slow", "root", "channeled", "squishy", "early_game", "magical", "support"],
    "Drow Ranger": ["silence", "push", "late_game", "squishy", "physical", "aura", "carry"],
    "Earthshaker": ["aoe", "stun", "initiator", "counter_initiate", "burst", "blink_reliant", "magical"],
    "Juggernaut": ["spell_immune", "heal", "push", "physical", "burst", "late_game", "carry"],
    "Mirana": ["invis", "global", "stun", "mobility", "aoe", "roam", "escape", "support"],
    "Morphling": ["escape", "mobility", "self_sustain", "physical", "burst", "late_game", "carry"],
    "Shadow Fiend": ["aoe", "burst", "magical", "wave_clear", "push", "squishy", "late_game", "carry"],
    "Phantom Lancer": ["illusions", "escape", "split_push", "late_game", "wave_clear", "physical", "carry", "sustained_dps"],
    "Puck": ["mobility", "escape", "silence", "aoe", "initiator", "squishy", "magical"],
    "Pudge": ["single_target", "initiator", "durable", "aoe", "burst", "gank"],
    "Razor": ["physical", "sustained_dps", "aoe", "early_game", "durable", "carry"],
    "Sand King": ["invis", "aoe", "stun", "initiator", "escape", "channeled", "teamfight", "counter_initiate", "support"],
    "Storm Spirit": ["mobility", "escape", "burst", "magical", "single_target", "late_game", "initiator", "carry"],
    "Sven": ["aoe", "stun", "burst", "physical", "initiator", "late_game", "durable", "carry"],
    "Tiny": ["burst", "aoe", "stun", "push", "physical", "durable", "initiator", "carry"],
    "Vengeful Spirit": ["single_target", "stun", "save", "initiator", "burst", "aura", "support"],
    "Windranger": ["single_target", "stun", "escape", "physical", "burst", "squishy", "support"],
    "Zeus": ["magical", "burst", "detection", "global", "aoe", "squishy", "wave_clear", "global_vision"],
    "Kunkka": ["aoe", "stun", "burst", "physical", "buff", "durable", "initiator"],
    "Lina": ["magical", "burst", "aoe", "squishy", "single_target", "wave_clear", "support"],
    "Lion": ["single_target", "hex", "stun", "burst", "magical", "squishy", "support"],
    "Shadow Shaman": ["hex", "stun", "push", "summons", "single_target", "disable", "support"],
    "Slardar": ["armor_reduction", "stun", "physical", "initiator", "durable", "detection", "global_vision"],
    "Tidehunter": ["aoe", "stun", "initiator", "counter_initiate", "durable", "teamfight", "high_armor"],
    "Witch Doctor": ["stun", "aoe", "single_target", "channeled", "heal", "squishy", "magical", "support"],
    "Lich": ["aoe", "slow", "shield_ally", "squishy", "magical", "wave_clear", "support"],
    "Riki": ["invis", "single_target", "burst", "escape", "physical", "late_game", "carry"],
    "Enigma": ["summons", "stun", "bkb_pierce", "initiator", "push", "teamfight", "aoe", "channeled"],
    "Tinker": ["magical", "burst", "push", "wave_clear", "mobility", "single_target", "late_game", "carry"],
    "Sniper": ["physical", "late_game", "squishy", "carry", "sustained_dps"],
    "Necrophos": ["aoe", "self_sustain", "single_target", "magical", "durable", "burst", "carry"],
    "Warlock": ["summons", "teamfight", "aoe", "heal", "initiator", "channeled", "support"],
    "Beastmaster": ["summons", "bkb_pierce", "stun", "vision", "push", "aura", "initiator", "durable", "global_vision"],
    "Queen of Pain": ["magical", "burst", "aoe", "escape", "mobility", "squishy", "single_target", "late_game", "carry"],
    "Venomancer": ["summons", "aoe", "slow", "push", "vision", "magical", "wave_clear", "support"],
    "Faceless Void": ["aoe", "bkb_pierce", "stun", "initiator", "teamfight", "physical", "late_game", "mobility", "counter_initiate", "carry"],
    "Wraith King": ["summons", "stun", "lifesteal", "self_sustain", "physical", "late_game", "carry"],
    "Death Prophet": ["push", "summons", "silence", "aoe", "self_sustain", "wave_clear", "carry"],
    "Phantom Assassin": ["evasion", "burst", "single_target", "physical", "mobility", "late_game", "carry"],
    "Pugna": ["magical", "burst", "push", "aoe", "single_target", "save", "wave_clear"],
    "Templar Assassin": ["evasion", "burst", "physical", "single_target", "shield", "mobility", "vision", "carry"],
    "Viper": ["break", "slow", "single_target", "sustained_dps", "physical", "durable", "carry"],
    "Luna": ["physical", "aoe", "late_game", "push", "squishy", "burst", "carry"],
    "Dragon Knight": ["durable", "stun", "push", "physical", "wave_clear", "initiator", "carry", "high_armor"],
    "Dazzle": ["save", "heal", "shield_ally", "magical", "support", "wave_clear"],
    "Clockwerk": ["single_target", "stun", "durable", "initiator", "mobility", "push"],
    "Leshrac": ["magical", "aoe", "push", "wave_clear", "stun", "sustained_dps", "squishy", "support"],
    "Nature's Prophet": ["summons", "push", "global", "split_push", "mobility", "aoe", "wave_clear", "carry"],
    "Lifestealer": ["lifesteal", "self_sustain", "physical", "spell_immune", "late_game", "durable", "carry"],
    "Dark Seer": ["aoe", "illusions", "teamfight", "aura", "mobility", "wave_clear", "initiator"],
    "Clinkz": ["invis", "summons", "physical", "burst", "push", "escape", "late_game", "carry"],
    "Omniknight": ["save", "heal", "dispel", "aura", "buff", "durable", "support"],
    "Enchantress": ["summons", "push", "slow", "squishy", "support"],
    "Huskar": ["self_sustain", "physical", "single_target", "durable", "early_game", "carry", "lifesteal"],
    "Night Stalker": ["global", "silence", "slow", "gank", "initiator", "durable", "burst", "vision", "carry"],
    "Broodmother": ["summons", "push", "escape", "physical", "wave_clear", "carry"],
    "Bounty Hunter": ["invis", "detection", "gank", "burst", "single_target", "vision", "global_vision"],
    "Weaver": ["mobility", "escape", "invis", "physical", "single_target", "late_game", "carry"],
    "Jakiro": ["aoe", "stun", "push", "wave_clear", "slow", "magical", "teamfight", "support"],
    "Batrider": ["single_target", "bkb_pierce", "initiator", "gank", "mobility", "aoe", "escape"],
    "Chen": ["summons", "heal", "push", "global", "aura", "save", "support"],
    "Spectre": ["late_game", "physical", "global", "illusions", "durable", "sustained_dps", "carry", "global_vision"],
    "Ancient Apparition": ["anti_heal", "global", "magical", "aoe", "slow", "burst", "support"],
    "Doom": ["bkb_pierce", "single_target", "durable", "initiator", "silence", "late_game", "physical", "break", "carry"],
    "Ursa": ["single_target", "physical", "burst", "early_game", "durable", "sustained_dps", "carry"],
    "Spirit Breaker": ["global", "stun", "initiator", "gank", "mobility", "durable", "bash"],
    "Gyrocopter": ["aoe", "magical", "late_game", "teamfight", "wave_clear", "burst", "carry"],
    "Alchemist": ["physical", "stun", "initiator", "durable", "self_sustain", "late_game", "armor_reduction", "carry"],
    "Invoker": ["aoe", "burst", "magical", "summons", "invis", "stun", "push", "global", "carry"],
    "Silencer": ["silence", "global", "disarm", "magical", "support"],
    "Outworld Destroyer": ["magical", "single_target", "burst", "save", "mana_burn", "late_game", "carry"],
    "Lycan": ["summons", "push", "physical", "escape", "split_push", "late_game", "aura", "carry"],
    "Brewmaster": ["aoe", "stun", "initiator", "teamfight", "evasion", "summons", "dispel", "durable", "counter_initiate"],
    "Shadow Demon": ["illusions", "save", "single_target", "magical", "disable", "support"],
    "Lone Druid": ["summons", "push", "physical", "durable", "late_game", "root", "carry"],
    "Chaos Knight": ["illusions", "burst", "single_target", "stun", "late_game", "physical", "durable", "carry", "mobility"],
    "Meepo": ["magical", "burst", "mobility", "single_target", "late_game", "wave_clear", "carry"],
    "Treant Protector": ["invis", "heal", "global", "save", "aoe", "root", "durable", "support", "vision"],
    "Ogre Magi": ["stun", "buff", "burst", "magical", "durable", "aoe", "support", "high_armor"],
    "Undying": ["summons", "heal", "durable", "teamfight", "aoe", "self_sustain", "aura", "support"],
    "Rubick": ["disable", "burst", "magical", "teamfight", "support"],
    "Disruptor": ["aoe", "silence", "initiator", "teamfight", "single_target", "burst", "support"],
    "Nyx Assassin": ["invis", "burst", "single_target", "stun", "mana_burn", "initiator", "mobility"],
    "Naga Siren": ["illusions", "push", "aoe", "save", "disable", "late_game", "split_push", "carry", "root"],
    "Keeper of the Light": ["magical", "burst", "push", "wave_clear", "global", "vision", "support"],
    "Io": ["heal", "save", "global", "buff", "mobility", "aura", "squishy", "support"],
    "Visage": ["summons", "push", "burst", "durable", "late_game", "support"],
    "Slark": ["escape", "physical", "single_target", "late_game", "mobility", "dispel", "invis", "carry"],
    "Medusa": ["durable", "late_game", "aoe", "physical", "sustained_dps", "stun", "carry"],
    "Troll Warlord": ["single_target", "physical", "late_game", "push", "stun", "aoe", "bash", "carry"],
    "Centaur Warrunner": ["durable", "stun", "initiator", "aoe", "mobility", "teamfight", "counter_initiate", "high_armor"],
    "Magnus": ["aoe", "stun", "bkb_pierce", "initiator", "mobility", "teamfight", "buff", "burst"],
    "Timbersaw": ["durable", "mobility", "aoe", "burst", "magical", "escape"],
    "Bristleback": ["durable", "physical", "aoe", "counter_initiate", "sustained_dps", "wave_clear", "high_armor"],
    "Tusk": ["single_target", "mobility", "save", "initiator", "burst", "slow", "stun"],
    "Skywrath Mage": ["magical", "burst", "silence", "single_target", "squishy", "support"],
    "Abaddon": ["save", "dispel", "heal", "durable", "physical", "shield_ally", "support"],
    "Elder Titan": ["aoe", "stun", "initiator", "teamfight", "aura", "armor_reduction", "sleep", "durable", "magical"],
    "Legion Commander": ["single_target", "physical", "burst", "disable", "self_sustain", "durable", "initiator"],
    "Techies": ["aoe", "burst", "magical", "push", "wave_clear", "trap", "stun"],
    "Ember Spirit": ["mobility", "escape", "physical", "magical", "aoe", "root", "late_game", "burst", "carry"],
    "Earth Spirit": ["aoe", "stun", "initiator", "mobility", "magical", "save", "silence", "root"],
    "Underlord": ["aoe", "wave_clear", "push", "teamfight", "aura", "global", "root", "durable", "high_armor"],
    "Terrorblade": ["illusions", "push", "physical", "late_game", "single_target", "split_push", "carry", "high_armor"],
    "Phoenix": ["aoe", "heal", "save", "teamfight", "initiator", "magical", "sustained_dps"],
    "Oracle": ["save", "heal", "dispel", "single_target", "burst", "disable", "magical", "root", "support"],
    "Winter Wyvern": ["save", "aoe", "magical", "disable", "teamfight", "wave_clear", "support"],
    "Arc Warden": ["illusions", "push", "split_push", "physical", "magical", "single_target", "late_game", "carry"],
    "Monkey King": ["mobility", "escape", "physical", "single_target", "burst", "late_game", "aoe", "carry"],
    "Dark Willow": ["aoe", "disable", "burst", "magical", "escape", "fear", "support"],
    "Pangolier": ["mobility", "aoe", "stun", "durable", "burst", "initiator", "escape"],
    "Grimstroke": ["aoe", "silence", "single_target", "burst", "magical", "disable", "support"],
    "Hoodwink": ["magical", "burst", "escape", "aoe", "stun", "squishy", "support", "mobility"],
    "Void Spirit": ["mobility", "burst", "magical", "aoe", "escape", "disable", "stun"],
    "Snapfire": ["aoe", "burst", "magical", "push", "disable", "save", "mobility", "stun", "support"],
    "Mars": ["aoe", "stun", "initiator", "durable", "teamfight", "disable", "counter_initiate", "stun"],
    "Ringmaster": ["aoe", "disable", "magical", "escape", "save", "invis", "support"],
    "Dawnbreaker": ["heal", "global", "teamfight", "durable", "physical", "aoe", "stun", "buff"],
    "Marci": ["single_target", "burst", "mobility", "disable", "physical", "initiator", "buff", "stun"],
    "Primal Beast": ["aoe", "stun", "initiator", "durable", "mobility", "burst", "disable"],
    "Muerta": ["magical", "burst", "disable", "aoe", "single_target", "late_game", "spell_immune", "carry", "physical"],
    "Kez": ["physical", "single_target", "burst", "mobility", "escape", "disable", "aoe", "carry"],
    "Largo": ["durable", "disable", "heal", "aoe", "magical", "teamfight", "save", "support"],
}

ROLE_TAGS = {
    "Initiator": "initiator",
    "Pusher": "push",
    "Durable": "durable",
    "Escape": "escape",
    "Support": "support",
    "Carry": "carry",
}


def load_raw():
    if os.path.exists(CACHE):
        print(f"using cached {CACHE}", file=sys.stderr)
        with open(CACHE) as f:
            return json.load(f)
    req = urllib.request.Request(
        "https://api.opendota.com/api/heroes",
        headers={"User-Agent": "counterdraft-hero-gen/1.0"},
    )
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)


def main():
    raw = load_raw()

    out = []
    for h in sorted(raw, key=lambda x: x["id"]):
        name = h["localized_name"]
        tags = set(TAGS.get(name, []))
        tags.update(ROLE_TAGS[r] for r in h["roles"] if r in ROLE_TAGS)
        tags.add("melee_heavy" if h["attack_type"] == "Melee" else "ranged")
        if name not in TAGS:
            print(f"WARNING: no hand-authored tags for {name}", file=sys.stderr)
        out.append({
            "id": h["id"],
            "name": h["name"],
            "localized_name": name,
            "attr": h["primary_attr"],
            "attack_type": h["attack_type"],
            "roles": h["roles"],
            "tags": sorted(tags),
        })

    json.dump(out, sys.stdout, indent=2)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
