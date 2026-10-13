package model

import "time"

// Difficulty 难度唯一枚举：apprentice/practitioner/expert
// （teaching-integration skill：禁止新增第四级，model/lab.yaml/前端三处必须一致）。
type Difficulty string

const (
	DifficultyApprentice  Difficulty = "apprentice"
	DifficultyPractitioner Difficulty = "practitioner"
	DifficultyExpert      Difficulty = "expert"
)

// Topic 主题（如 SQLi/XSS），内容层级：Topic → Room（教题一体，teaching-integration skill）。
type Topic struct {
	ID        uint      `gorm:"primaryKey"`
	Slug      string    `gorm:"uniqueIndex;size:128" json:"slug"`
	Title     string    `gorm:"size:256" json:"title"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Room 学习的原子单元：文字教程正文 + 可选视频教程。
// 题目是独立资源（Challenge.TutorialSlug 可选指回本 Room），不再内嵌 Lab。
type Room struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	TopicSlug  string     `gorm:"index;size:128" json:"topic_slug"`
	Slug       string     `gorm:"uniqueIndex;size:128" json:"slug"`
	Title      string     `gorm:"size:256" json:"title"`
	Difficulty Difficulty `gorm:"size:16" json:"difficulty"`
	LessonMD   string     `json:"-"` // 教程正文（Markdown），由 Room 详情接口单独返回
	// VideoURL 视频教程地址（可选，lesson.md front matter video 字段）。
	VideoURL string `gorm:"size:512" json:"video_url"`
	// Requirements 前置依赖（Room 级解锁图，JSON 数组存 room slug 列表）。
	Requirements string `gorm:"type:text" json:"requirements,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
