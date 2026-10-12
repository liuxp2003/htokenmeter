package model

import (
	"errors"
	"math/rand"
	"sort"

	"gorm.io/gorm"
)

// Ability is the routing index: one row per (group, model, channel).
type Ability struct {
	Group     string `json:"group" gorm:"primaryKey;size:32"`
	Model     string `json:"model" gorm:"primaryKey;size:128"`
	ChannelId int    `json:"channel_id" gorm:"primaryKey"`
	Enabled   bool   `json:"enabled"`
	Priority  int    `json:"priority"`
	Weight    int    `json:"weight"`
}

// UpdateAbilities rebuilds this channel's rows from Models × Group.
func (ch *Channel) UpdateAbilities() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("channel_id = ?", ch.Id).Delete(&Ability{}).Error; err != nil {
			return err
		}
		seen := map[string]bool{}
		var rows []Ability
		for _, m := range ch.ModelList() {
			for _, g := range splitComma(ch.Group) {
				k := g + "\x00" + m
				if seen[k] {
					continue
				}
				seen[k] = true
				rows = append(rows, Ability{
					Group: g, Model: m, ChannelId: ch.Id,
					Enabled:  ch.Status == ChannelStatusEnabled,
					Priority: ch.Priority, Weight: ch.Weight,
				})
			}
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
}

func UpdateAbilityStatus(channelId int, enabled bool) error {
	return DB.Model(&Ability{}).Where("channel_id = ?", channelId).Update("enabled", enabled).Error
}

// FixAbility rebuilds every channel's abilities; runs at startup so orphan or
// missing rows self-heal (the one-api "database consistency broken" class of bugs).
func FixAbility() error {
	var channels []Channel
	if err := DB.Find(&channels).Error; err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM abilities WHERE channel_id NOT IN (SELECT id FROM channels)").Error; err != nil {
			return err
		}
		for i := range channels {
			ch := channels[i]
			if err := tx.Where("channel_id = ?", ch.Id).Delete(&Ability{}).Error; err != nil {
				return err
			}
			seen := map[string]bool{}
			var rows []Ability
			for _, m := range ch.ModelList() {
				for _, g := range splitComma(ch.Group) {
					k := g + "\x00" + m
					if seen[k] {
						continue
					}
					seen[k] = true
					rows = append(rows, Ability{
						Group: g, Model: m, ChannelId: ch.Id,
						Enabled:  ch.Status == ChannelStatusEnabled,
						Priority: ch.Priority, Weight: ch.Weight,
					})
				}
			}
			if len(rows) > 0 {
				if err := tx.Create(&rows).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// errConsistency mirrors one-api's explicit orphan-ability failure.
var errConsistency = errors.New("database consistency error: channel missing, please contact admin")

// GetRandomSatisfiedChannel picks a channel for group+model.
// retry selects the (retry+1)-th priority tier; within the tier channels are
// picked by weighted random with uniform fallback when all weights are zero.
func GetRandomSatisfiedChannel(group, model string, retry int) (*Channel, error) {
	var abs []Ability
	if err := DB.Where("`group` = ? AND model = ? AND enabled = ?", group, model, true).
		Find(&abs).Error; err != nil {
		return nil, err
	}
	if len(abs) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	// Distinct priority tiers, descending.
	tiers := map[int]bool{}
	for _, a := range abs {
		tiers[a.Priority] = true
	}
	sorted := make([]int, 0, len(tiers))
	for p := range tiers {
		sorted = append(sorted, p)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sorted)))
	if retry >= len(sorted) {
		retry = len(sorted) - 1
	}
	target := sorted[retry]

	var candidates []Ability
	for _, a := range abs {
		if a.Priority == target {
			candidates = append(candidates, a)
		}
	}
	if len(candidates) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	pick := weightedPick(candidates)
	var ch Channel
	if err := DB.First(&ch, pick.ChannelId).Error; err != nil {
		return nil, errConsistency
	}
	return &ch, nil
}

func weightedPick(candidates []Ability) Ability {
	sum := 0
	for _, a := range candidates {
		if a.Weight > 0 {
			sum += a.Weight
		}
	}
	if sum == 0 {
		return candidates[rand.Intn(len(candidates))]
	}
	r := rand.Intn(sum)
	for _, a := range candidates {
		if a.Weight <= 0 {
			continue
		}
		r -= a.Weight
		if r < 0 {
			return a
		}
	}
	return candidates[len(candidates)-1]
}

// GroupModels lists models available to a group (sorted, deduplicated).
func GroupModels(group string) ([]string, error) {
	var models []string
	err := DB.Model(&Ability{}).Distinct().
		Where("`group` = ? AND enabled = ?", group, true).
		Order("model asc").Pluck("model", &models).Error
	return models, err
}

// AllGroups lists distinct groups across channels.
func AllGroups() []string {
	var groups []string
	DB.Model(&Channel{}).Distinct().Pluck("`group`", &groups)
	seen := map[string]bool{}
	var out []string
	for _, g := range groups {
		for _, part := range splitComma(g) {
			if !seen[part] {
				seen[part] = true
				out = append(out, part)
			}
		}
	}
	return out
}
