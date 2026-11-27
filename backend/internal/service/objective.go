package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// 基础题（训练场客观题）的规范化与判分。
// 与考试域共用“选项/答案”的纯逻辑，但训练场判题在 worker 异步完成，
// 部分分只用于作答反馈，不进入 submission.judged 的派生计分
//（event-sourcing-rules skill）。

const (
	maxQuestionOptions     = 26
	maxQuestionOptionText  = 500
	maxQuestionExplanation = 16 << 10
	maxObjectiveAnswer     = 4 << 10
)

var questionOptionKeyRe = regexp.MustCompile(`^[A-Z0-9]{1,8}$`)

// QuestionOption 客观题选项（管理端/学生端同构）。
type QuestionOption struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

// QuestionMatch 填空匹配策略。默认：忽略首尾空白、不区分大小写、非正则。
type QuestionMatch struct {
	CaseSensitive bool `json:"case_sensitive"`
	Trim          bool `json:"trim"`
	Regex         bool `json:"regex"`
}

// questionMatchInput 上传 DTO 的三态布尔（nil = 使用默认值）。
type questionMatchInput struct {
	CaseSensitive *bool `json:"case_sensitive" yaml:"case_sensitive"`
	Trim          *bool `json:"trim" yaml:"trim"`
	Regex         *bool `json:"regex" yaml:"regex"`
}

// questionSpec 解析后的作答定义（答案 JSON 为规范形态）。
type questionSpec struct {
	Type        model.ChallengeType
	Options     []QuestionOption
	AnswerJSON  string
	Match       QuestionMatch
	Explanation string
}

// isObjectiveChallengeType 是否基础题类型。
func isObjectiveChallengeType(t model.ChallengeType) bool {
	switch t {
	case model.ChallengeSingle, model.ChallengeMultiple, model.ChallengeTrueFalse, model.ChallengeFill:
		return true
	}
	return false
}

// normalizeQuestionDefinition 校验并规范化一道基础题定义。
// options/answer/match 的语义随 type 变化；answer 为上传原始 JSON。
func normalizeQuestionDefinition(
	typ model.ChallengeType,
	options []QuestionOption,
	rawAnswer json.RawMessage,
	rawMatch *questionMatchInput,
	explanation string,
) (*model.ChallengeQuestion, *questionSpec, error) {
	if !isObjectiveChallengeType(typ) {
		return nil, nil, fmt.Errorf("%w: question.type must be single, multiple, truefalse or fill", ErrValidation)
	}
	if len(rawAnswer) == 0 {
		return nil, nil, fmt.Errorf("%w: question.answer is required", ErrValidation)
	}
	if len(explanation) > maxQuestionExplanation {
		return nil, nil, fmt.Errorf("%w: question.explanation exceeds 16KiB", ErrValidation)
	}

	var normalizedOptions []QuestionOption
	var answerJSON string
	var match QuestionMatch
	var err error

	switch typ {
	case model.ChallengeSingle, model.ChallengeMultiple:
		normalizedOptions, err = normalizeQuestionOptions(options)
		if err != nil {
			return nil, nil, err
		}
		answerJSON, err = normalizeChoiceAnswer(typ, normalizedOptions, rawAnswer)
		if err != nil {
			return nil, nil, err
		}
		if rawMatch != nil {
			return nil, nil, fmt.Errorf("%w: question.match is only supported for fill questions", ErrValidation)
		}
	case model.ChallengeTrueFalse:
		if len(options) > 0 {
			return nil, nil, fmt.Errorf("%w: truefalse questions do not accept options", ErrValidation)
		}
		if rawMatch != nil {
			return nil, nil, fmt.Errorf("%w: question.match is only supported for fill questions", ErrValidation)
		}
		answerJSON, err = normalizeTrueFalseAnswer(rawAnswer)
		if err != nil {
			return nil, nil, err
		}
		normalizedOptions = []QuestionOption{}
	case model.ChallengeFill:
		if len(options) > 0 {
			return nil, nil, fmt.Errorf("%w: fill questions do not accept options", ErrValidation)
		}
		answerJSON, err = normalizeFillAnswer(rawAnswer)
		if err != nil {
			return nil, nil, err
		}
		match = defaultQuestionMatch()
		if rawMatch != nil {
			match = QuestionMatch{
				CaseSensitive: rawMatch.CaseSensitive != nil && *rawMatch.CaseSensitive,
				Trim:          rawMatch.Trim == nil || *rawMatch.Trim,
				Regex:         rawMatch.Regex != nil && *rawMatch.Regex,
			}
		}
		if match.Regex {
			if err := validateFillRegexAnswers(answerJSON, match); err != nil {
				return nil, nil, err
			}
		}
		normalizedOptions = []QuestionOption{}
	}

	optionsJSON, err := json.Marshal(normalizedOptions)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: question.options: %v", ErrValidation, err)
	}
	matchJSON := ""
	if typ == model.ChallengeFill {
		encoded, err := json.Marshal(match)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: question.match: %v", ErrValidation, err)
		}
		matchJSON = string(encoded)
	}
	q := &model.ChallengeQuestion{
		OptionsJSON: string(optionsJSON),
		AnswerJSON:  answerJSON,
		MatchJSON:   matchJSON,
		Explanation: strings.TrimSpace(explanation),
	}
	return q, &questionSpec{
		Type: typ, Options: normalizedOptions, AnswerJSON: answerJSON,
		Match: match, Explanation: q.Explanation,
	}, nil
}

// normalizeQuestionOptions 校验选项：2..26 个、key 大写字母/数字、text 非空。
func normalizeQuestionOptions(options []QuestionOption) ([]QuestionOption, error) {
	if len(options) < 2 || len(options) > maxQuestionOptions {
		return nil, fmt.Errorf("%w: questions require 2..%d options", ErrValidation, maxQuestionOptions)
	}
	seen := make(map[string]bool, len(options))
	out := make([]QuestionOption, 0, len(options))
	for _, option := range options {
		key := strings.TrimSpace(option.Key)
		text := strings.TrimSpace(option.Text)
		if !questionOptionKeyRe.MatchString(key) {
			return nil, fmt.Errorf("%w: invalid option key %q (uppercase letters/digits, 1-8)", ErrValidation, option.Key)
		}
		if seen[key] {
			return nil, fmt.Errorf("%w: duplicate option key %q", ErrValidation, key)
		}
		if text == "" || len(text) > maxQuestionOptionText {
			return nil, fmt.Errorf("%w: option %q text is required and must be <=500 characters", ErrValidation, key)
		}
		seen[key] = true
		out = append(out, QuestionOption{Key: key, Text: text})
	}
	return out, nil
}

// normalizeChoiceAnswer 单选/多选答案规范化（答案必须是已声明的选项 key）。
func normalizeChoiceAnswer(typ model.ChallengeType, options []QuestionOption, raw json.RawMessage) (string, error) {
	switch typ {
	case model.ChallengeSingle:
		var key string
		if err := json.Unmarshal(raw, &key); err != nil {
			return "", fmt.Errorf("%w: single answer must be a string option key", ErrValidation)
		}
		key = strings.TrimSpace(key)
		if !questionOptionExists(options, key) {
			return "", fmt.Errorf("%w: answer key %q is not an option", ErrValidation, key)
		}
		encoded, _ := json.Marshal(key)
		return string(encoded), nil
	case model.ChallengeMultiple:
		var keys []string
		if err := json.Unmarshal(raw, &keys); err != nil {
			return "", fmt.Errorf("%w: multiple answer must be an array of option keys", ErrValidation)
		}
		if len(keys) == 0 {
			return "", fmt.Errorf("%w: multiple answer cannot be empty", ErrValidation)
		}
		seen := make(map[string]bool, len(keys))
		normalized := make([]string, 0, len(keys))
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if !questionOptionExists(options, key) {
				return "", fmt.Errorf("%w: answer key %q is not an option", ErrValidation, key)
			}
			if seen[key] {
				return "", fmt.Errorf("%w: duplicate answer key %q", ErrValidation, key)
			}
			seen[key] = true
			normalized = append(normalized, key)
		}
		sort.Strings(normalized)
		encoded, _ := json.Marshal(normalized)
		return string(encoded), nil
	}
	return "", fmt.Errorf("%w: invalid choice question type", ErrValidation)
}

func normalizeTrueFalseAnswer(raw json.RawMessage) (string, error) {
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%w: truefalse answer must be true or false", ErrValidation)
	}
	encoded, _ := json.Marshal(value)
	return string(encoded), nil
}

// normalizeFillAnswer 填空答案：一个字符串或字符串数组（多个可接受答案）。
func normalizeFillAnswer(raw json.RawMessage) (string, error) {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		single = strings.TrimSpace(single)
		if single == "" {
			return "", fmt.Errorf("%w: fill answer cannot be empty", ErrValidation)
		}
		encoded, _ := json.Marshal([]string{single})
		return string(encoded), nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return "", fmt.Errorf("%w: fill answer must be a string or an array of strings", ErrValidation)
	}
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for _, answer := range list {
		answer = strings.TrimSpace(answer)
		if answer == "" {
			return "", fmt.Errorf("%w: fill answer cannot be empty", ErrValidation)
		}
		if seen[answer] {
			continue
		}
		seen[answer] = true
		out = append(out, answer)
	}
	if len(out) == 0 {
		return "", fmt.Errorf("%w: fill answer cannot be empty", ErrValidation)
	}
	encoded, _ := json.Marshal(out)
	return string(encoded), nil
}

func defaultQuestionMatch() QuestionMatch {
	return QuestionMatch{CaseSensitive: false, Trim: true, Regex: false}
}

func validateFillRegexAnswers(answerJSON string, match QuestionMatch) error {
	var answers []string
	if err := json.Unmarshal([]byte(answerJSON), &answers); err != nil {
		return fmt.Errorf("%w: fill answer: %v", ErrValidation, err)
	}
	for _, answer := range answers {
		pattern := answer
		if !match.CaseSensitive {
			pattern = "(?i)" + pattern
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("%w: invalid fill regex %q: %v", ErrValidation, answer, err)
		}
	}
	return nil
}

// parseQuestionSpec 从库内 JSON 列解析作答定义（判题/详情读取共用）。
func parseQuestionSpec(typ model.ChallengeType, q *model.ChallengeQuestion) (*questionSpec, error) {
	if q == nil {
		return nil, fmt.Errorf("%w: question definition missing", ErrValidation)
	}
	spec := &questionSpec{Type: typ, AnswerJSON: q.AnswerJSON, Explanation: q.Explanation}
	if q.OptionsJSON != "" {
		if err := json.Unmarshal([]byte(q.OptionsJSON), &spec.Options); err != nil {
			return nil, fmt.Errorf("%w: question options corrupt: %v", ErrValidation, err)
		}
	}
	if typ == model.ChallengeFill && q.MatchJSON != "" {
		if err := json.Unmarshal([]byte(q.MatchJSON), &spec.Match); err != nil {
			return nil, fmt.Errorf("%w: question match corrupt: %v", ErrValidation, err)
		}
	} else {
		spec.Match = defaultQuestionMatch()
	}
	return spec, nil
}

func questionOptionExists(options []QuestionOption, key string) bool {
	for _, option := range options {
		if option.Key == key {
			return true
		}
	}
	return false
}

// normalizeObjectiveSubmission 受理前校验并规范化学员提交的答案。
// 返回存入 Submission.Answer 的规范 JSON；非法形状直接 422。
func normalizeObjectiveSubmission(typ model.ChallengeType, spec *questionSpec, raw string) (string, error) {
	if len(raw) == 0 || len(raw) > maxObjectiveAnswer {
		return "", fmt.Errorf("%w: answer size must be 1..%d bytes", ErrValidation, maxObjectiveAnswer)
	}
	switch typ {
	case model.ChallengeSingle:
		var key string
		if err := json.Unmarshal([]byte(raw), &key); err != nil {
			return "", fmt.Errorf("%w: single answer must be a JSON string option key", ErrValidation)
		}
		key = strings.TrimSpace(key)
		if !questionOptionExists(spec.Options, key) {
			return "", fmt.Errorf("%w: answer key %q is not an option", ErrValidation, key)
		}
		encoded, _ := json.Marshal(key)
		return string(encoded), nil
	case model.ChallengeMultiple:
		var keys []string
		if err := json.Unmarshal([]byte(raw), &keys); err != nil {
			return "", fmt.Errorf("%w: multiple answer must be a JSON array of option keys", ErrValidation)
		}
		if len(keys) == 0 {
			return "", fmt.Errorf("%w: multiple answer cannot be empty", ErrValidation)
		}
		seen := make(map[string]bool, len(keys))
		out := make([]string, 0, len(keys))
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if !questionOptionExists(spec.Options, key) {
				return "", fmt.Errorf("%w: answer key %q is not an option", ErrValidation, key)
			}
			if seen[key] {
				return "", fmt.Errorf("%w: duplicate answer key %q", ErrValidation, key)
			}
			seen[key] = true
			out = append(out, key)
		}
		sort.Strings(out)
		encoded, _ := json.Marshal(out)
		return string(encoded), nil
	case model.ChallengeTrueFalse:
		var value bool
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return "", fmt.Errorf("%w: truefalse answer must be true or false", ErrValidation)
		}
		encoded, _ := json.Marshal(value)
		return string(encoded), nil
	case model.ChallengeFill:
		var value string
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return "", fmt.Errorf("%w: fill answer must be a JSON string", ErrValidation)
		}
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%w: fill answer cannot be empty", ErrValidation)
		}
		encoded, _ := json.Marshal(value)
		return string(encoded), nil
	}
	return "", fmt.Errorf("%w: invalid question type", ErrValidation)
}

// gradeObjective 判定 objective 答案；返回是否全对与本次应得分。
// fullScore 为当前动态题分（部分分只做反馈，不进入派生表）。
func gradeObjective(typ model.ChallengeType, spec *questionSpec, gotJSON string, fullScore int) (bool, int) {
	if fullScore < 0 {
		fullScore = 0
	}
	switch typ {
	case model.ChallengeSingle:
		if objectiveAnswerCorrect(typ, spec.AnswerJSON, gotJSON) {
			return true, fullScore
		}
		return false, 0
	case model.ChallengeTrueFalse:
		correct := objectiveAnswerCorrect(typ, spec.AnswerJSON, gotJSON)
		if correct {
			return true, fullScore
		}
		return false, 0
	case model.ChallengeMultiple:
		return multipleChoiceScore(spec.AnswerJSON, gotJSON, fullScore)
	case model.ChallengeFill:
		if fillAnswerCorrect(spec.AnswerJSON, gotJSON, spec.Match) {
			return true, fullScore
		}
		return false, 0
	}
	return false, 0
}

// objectiveAnswerCorrect 单选/多选/判断的纯比对（多选集合相等、顺序无关）。
func objectiveAnswerCorrect(typ model.ChallengeType, answerJSON, gotJSON string) bool {
	switch typ {
	case model.ChallengeSingle:
		var want, got string
		if json.Unmarshal([]byte(answerJSON), &want) != nil ||
			json.Unmarshal([]byte(gotJSON), &got) != nil {
			return false
		}
		return want == got
	case model.ChallengeMultiple:
		var want, got []string
		if json.Unmarshal([]byte(answerJSON), &want) != nil ||
			json.Unmarshal([]byte(gotJSON), &got) != nil {
			return false
		}
		return stringSetEqual(want, got)
	case model.ChallengeTrueFalse:
		var want, got bool
		if json.Unmarshal([]byte(answerJSON), &want) != nil ||
			json.Unmarshal([]byte(gotJSON), &got) != nil {
			return false
		}
		return want == got
	}
	return false
}

func stringSetEqual(a, b []string) bool {
	as := uniqueSorted(a)
	bs := uniqueSorted(b)
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// multipleChoiceScore 等权部分分：
// earned = round(fullScore × max(0, 选对项数 - 选错项数) / 正确项总数)。
// 全对（选中集合与正确集合一致）才算通关。
func multipleChoiceScore(answerJSON, gotJSON string, fullScore int) (bool, int) {
	var want, got []string
	if json.Unmarshal([]byte(answerJSON), &want) != nil ||
		json.Unmarshal([]byte(gotJSON), &got) != nil {
		return false, 0
	}
	wantSet := make(map[string]bool, len(want))
	for _, key := range want {
		wantSet[key] = true
	}
	if len(wantSet) == 0 {
		return false, 0
	}
	gotSet := make(map[string]bool, len(got))
	for _, key := range got {
		gotSet[key] = true
	}
	correctSelected, wrongSelected := 0, 0
	for key := range gotSet {
		if wantSet[key] {
			correctSelected++
		} else {
			wrongSelected++
		}
	}
	units := correctSelected - wrongSelected
	if units < 0 {
		units = 0
	}
	if units == len(wantSet) {
		return true, fullScore
	}
	earned := (fullScore*units + len(wantSet)/2) / len(wantSet)
	if earned < 0 {
		earned = 0
	}
	if earned > fullScore {
		earned = fullScore
	}
	return false, earned
}

// fillAnswerCorrect 填空匹配：可接受答案数组；默认 trim + 忽略大小写。
func fillAnswerCorrect(answerJSON, gotJSON string, match QuestionMatch) bool {
	var answers []string
	if json.Unmarshal([]byte(answerJSON), &answers) != nil {
		return false
	}
	var got string
	if json.Unmarshal([]byte(gotJSON), &got) != nil {
		return false
	}
	if match.Trim {
		got = strings.TrimSpace(got)
	}
	for _, answer := range answers {
		candidate := answer
		if match.Trim {
			candidate = strings.TrimSpace(candidate)
		}
		if match.Regex {
			pattern := candidate
			if !match.CaseSensitive {
				pattern = "(?i)" + pattern
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				continue
			}
			if re.MatchString(got) {
				return true
			}
			continue
		}
		if !match.CaseSensitive {
			if strings.EqualFold(got, candidate) {
				return true
			}
			continue
		}
		if got == candidate {
			return true
		}
	}
	return false
}
