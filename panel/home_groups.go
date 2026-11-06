package main

import "encoding/json"

type homeReferences []string

func (r *homeReferences) UnmarshalJSON(raw []byte) error { *r = homeMemberIDs(raw); return nil }

// Quasar returns references as either IDs or {id:...}; never copy whole
// nested device objects into groups or expose unrelated account metadata.
func homeMemberIDs(raw json.RawMessage) []string {
	var values []json.RawMessage
	_ = json.Unmarshal(raw, &values)
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		var id string
		if json.Unmarshal(value, &id) != nil {
			var ref struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(value, &ref)
			id = ref.ID
		}
		if homeID.MatchString(id) && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}
