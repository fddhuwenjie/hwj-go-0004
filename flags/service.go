package flags

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

const localTimeLayout = "2006-01-02T15:04:05"

type requestRecord struct {
	operation string
	result    []Flag
}

// Service is a concurrency-safe in-memory feature-flag store and evaluator.
type Service struct {
	mu       sync.Mutex
	flags    map[string]Flag
	requests map[string]requestRecord
	audit    []AuditEvent
	sequence int64
}

func NewService() *Service {
	return &Service{flags: make(map[string]Flag), requests: make(map[string]requestRecord)}
}

// Upsert validates and stores one flag. A non-empty request ID is idempotent.
func (s *Service) Upsert(flag Flag, requestID string) (Flag, error) {
	flag.Key = strings.TrimSpace(flag.Key)
	flag.Version = 0
	if err := validateFlag(flag); err != nil {
		return Flag{}, err
	}
	requestID = strings.TrimSpace(requestID)
	s.mu.Lock()
	defer s.mu.Unlock()
	operation := operationFingerprint("upsert", flag)
	if requestID != "" {
		if prior, ok := s.requests[requestID]; ok {
			if prior.operation != operation {
				return Flag{}, ErrRequestConflict
			}
			return cloneFlag(prior.result[0]), nil
		}
	}
	flag.Version = s.flags[flag.Key].Version + 1
	stored := cloneFlag(flag)
	s.flags[flag.Key] = stored
	if requestID != "" {
		s.requests[requestID] = requestRecord{operation: operation, result: []Flag{stored}}
	}
	return cloneFlag(stored), nil
}

// Import atomically validates and stores a batch. Duplicate keys are rejected.
func (s *Service) Import(batch []Flag, requestID string) ([]Flag, error) {
	if len(batch) == 0 {
		return nil, fmt.Errorf("%w: empty import", ErrInvalidInput)
	}
	normalized := make([]Flag, len(batch))
	seen := make(map[string]struct{}, len(batch))
	for i, flag := range batch {
		flag.Key = strings.TrimSpace(flag.Key)
		flag.Version = 0
		if err := validateFlag(flag); err != nil {
			return nil, fmt.Errorf("flag %d: %w", i, err)
		}
		if _, exists := seen[flag.Key]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateFlag, flag.Key)
		}
		seen[flag.Key] = struct{}{}
		normalized[i] = flag
	}
	requestID = strings.TrimSpace(requestID)
	s.mu.Lock()
	defer s.mu.Unlock()
	operation := importOperation(normalized)
	if requestID != "" {
		if prior, ok := s.requests[requestID]; ok {
			if prior.operation != operation {
				return nil, ErrRequestConflict
			}
			return cloneFlags(prior.result), nil
		}
	}
	result := make([]Flag, len(normalized))
	for i, flag := range normalized {
		flag.Version = s.flags[flag.Key].Version + 1
		stored := cloneFlag(flag)
		s.flags[flag.Key] = stored
		result[i] = stored
	}
	if requestID != "" {
		s.requests[requestID] = requestRecord{operation: operation, result: cloneFlags(result)}
	}
	return cloneFlags(result), nil
}

func importOperation(flags []Flag) string {
	return operationFingerprint("import", flags)
}

func operationFingerprint(kind string, value any) string {
	payload, _ := json.Marshal(value)
	return fmt.Sprintf("%s:%x", kind, sha256.Sum256(payload))
}

// Get returns an isolated copy of one flag.
func (s *Service) Get(key string) (Flag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flag, ok := s.flags[strings.TrimSpace(key)]
	if !ok {
		return Flag{}, ErrFlagNotFound
	}
	return cloneFlag(flag), nil
}

// Evaluate resolves prerequisites, schedule, targeting rules, and rollout.
func (s *Service) Evaluate(key string, context Context, at time.Time) (Evaluation, error) {
	key = strings.TrimSpace(key)
	context.SubjectKey = strings.TrimSpace(context.SubjectKey)
	if key == "" || context.SubjectKey == "" || at.IsZero() {
		return Evaluation{}, fmt.Errorf("%w: key, subjectKey and time are required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.evaluateLocked(key, context, at, make(map[string]bool))
	if err != nil {
		return Evaluation{}, err
	}
	s.sequence++
	s.audit = append(s.audit, AuditEvent{
		Sequence: s.sequence, FlagKey: key, SubjectKey: context.SubjectKey,
		Value: result.Value, Reason: result.Reason, At: at,
	})
	return result, nil
}

func (s *Service) evaluateLocked(key string, context Context, at time.Time, visiting map[string]bool) (Evaluation, error) {
	flag, ok := s.flags[key]
	if !ok {
		return Evaluation{}, fmt.Errorf("%w: %s", ErrFlagNotFound, key)
	}
	if visiting[key] {
		return Evaluation{}, fmt.Errorf("%w: %s", ErrPrerequisiteCycle, key)
	}
	visiting[key] = true
	defer delete(visiting, key)
	base := Evaluation{FlagKey: key, Value: flag.Default, Version: flag.Version, EvaluatedAt: at}
	if !flag.Enabled {
		base.Reason = "disabled"
		return base, nil
	}
	active, err := scheduleActive(flag.Schedule, at)
	if err != nil {
		return Evaluation{}, err
	}
	if !active {
		base.Reason = "outside_schedule"
		return base, nil
	}
	for _, prerequisite := range flag.Prerequisites {
		dependency, err := s.evaluateLocked(prerequisite.Key, context, at, visiting)
		if err != nil {
			return Evaluation{}, err
		}
		if dependency.Value != prerequisite.Value {
			base.Reason = "prerequisite_failed"
			return base, nil
		}
	}
	rules := append([]Rule(nil), flag.Rules...)
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Priority < rules[j].Priority })
	for _, rule := range rules {
		if ruleMatches(rule, context.Attributes) {
			base.Value, base.Reason = rule.Value, "rule:"+rule.ID
			return base, nil
		}
	}
	if len(flag.Rollout) > 0 {
		bucket := rolloutBucket(flag.Key, context.SubjectKey)
		cursor := 0
		for _, option := range flag.Rollout {
			cursor += option.Weight
			if bucket < cursor {
				base.Value, base.Reason = option.Value, "rollout"
				return base, nil
			}
		}
	}
	base.Reason = "default"
	return base, nil
}

func scheduleActive(schedule *Schedule, at time.Time) (bool, error) {
	if schedule == nil {
		return true, nil
	}
	location, err := time.LoadLocation(strings.TrimSpace(schedule.Location))
	if err != nil {
		return false, fmt.Errorf("%w: location: %v", ErrInvalidInput, err)
	}
	start, err := time.ParseInLocation(localTimeLayout, schedule.Start, location)
	if err != nil {
		return false, fmt.Errorf("%w: schedule start", ErrInvalidInput)
	}
	end, err := time.ParseInLocation(localTimeLayout, schedule.End, location)
	if err != nil || !start.Before(end) {
		return false, fmt.Errorf("%w: schedule end", ErrInvalidInput)
	}
	return !at.Before(start) && at.Before(end), nil
}

func ruleMatches(rule Rule, attributes map[string]string) bool {
	for _, condition := range rule.Conditions {
		actual := attributes[condition.Attribute]
		matched := false
		for _, expected := range condition.Values {
			if actual == expected {
				matched = true
				break
			}
		}
		if (condition.Operator == "equals" && !matched) || (condition.Operator == "not_equals" && matched) {
			return false
		}
	}
	return true
}

func rolloutBucket(flagKey, subjectKey string) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(flagKey + "\x00" + subjectKey))
	return int(hash.Sum32() % 10000)
}

// QueryAudit returns isolated events in insertion order.
func (s *Service) QueryAudit(query AuditQuery) ([]AuditEvent, error) {
	if query.Limit < 0 || (!query.From.IsZero() && !query.To.IsZero() && query.To.Before(query.From)) {
		return nil, fmt.Errorf("%w: audit query", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]AuditEvent, 0)
	for _, event := range s.audit {
		if query.FlagKey != "" && event.FlagKey != query.FlagKey {
			continue
		}
		if query.SubjectKey != "" && event.SubjectKey != query.SubjectKey {
			continue
		}
		if !query.From.IsZero() && event.At.Before(query.From) {
			continue
		}
		if !query.To.IsZero() && !event.At.Before(query.To) {
			continue
		}
		result = append(result, event)
		if query.Limit > 0 && len(result) == query.Limit {
			break
		}
	}
	return append([]AuditEvent(nil), result...), nil
}

func validateFlag(flag Flag) error {
	if flag.Key == "" || strings.TrimSpace(flag.Default) == "" {
		return fmt.Errorf("%w: key and default are required", ErrInvalidInput)
	}
	priorities := make(map[int]struct{}, len(flag.Rules))
	for _, rule := range flag.Rules {
		if strings.TrimSpace(rule.ID) == "" || strings.TrimSpace(rule.Value) == "" || len(rule.Conditions) == 0 {
			return fmt.Errorf("%w: incomplete rule", ErrInvalidInput)
		}
		if _, exists := priorities[rule.Priority]; exists {
			return fmt.Errorf("%w: duplicate rule priority", ErrInvalidInput)
		}
		priorities[rule.Priority] = struct{}{}
		for _, condition := range rule.Conditions {
			if condition.Attribute == "" || len(condition.Values) == 0 || (condition.Operator != "equals" && condition.Operator != "not_equals") {
				return fmt.Errorf("%w: invalid condition", ErrInvalidInput)
			}
		}
	}
	total := 0
	for _, option := range flag.Rollout {
		if option.Value == "" || option.Weight <= 0 {
			return fmt.Errorf("%w: invalid rollout", ErrInvalidInput)
		}
		total += option.Weight
	}
	if len(flag.Rollout) > 0 && total != 10000 {
		return fmt.Errorf("%w: rollout weights must total 10000", ErrInvalidInput)
	}
	for _, prerequisite := range flag.Prerequisites {
		if strings.TrimSpace(prerequisite.Key) == "" || prerequisite.Key == flag.Key || prerequisite.Value == "" {
			return fmt.Errorf("%w: invalid prerequisite", ErrInvalidInput)
		}
	}
	if flag.Schedule != nil {
		if _, err := scheduleActive(flag.Schedule, time.Now()); err != nil {
			return err
		}
	}
	return nil
}

func cloneFlags(flags []Flag) []Flag {
	result := make([]Flag, len(flags))
	for i, flag := range flags {
		result[i] = cloneFlag(flag)
	}
	return result
}

func cloneFlag(flag Flag) Flag {
	flag.Rules = append([]Rule(nil), flag.Rules...)
	for i := range flag.Rules {
		flag.Rules[i].Conditions = append([]Condition(nil), flag.Rules[i].Conditions...)
		for j := range flag.Rules[i].Conditions {
			flag.Rules[i].Conditions[j].Values = append([]string(nil), flag.Rules[i].Conditions[j].Values...)
		}
	}
	flag.Rollout = append([]RolloutOption(nil), flag.Rollout...)
	flag.Prerequisites = append([]Prerequisite(nil), flag.Prerequisites...)
	if flag.Schedule != nil {
		schedule := *flag.Schedule
		flag.Schedule = &schedule
	}
	return flag
}
