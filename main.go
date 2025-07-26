package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	_ "modernc.org/sqlite"
)

// --- CONFIGURATION ---

const (
	scenariosDir = "scenarios"
	dbPath       = "finyap.db"
)

var CLITICS = []string{"kaan", "kään", "kin", "han", "hän", "ko", "kö", "pa", "pä"}

// --- STYLING (using Lipgloss) ---

var (
	styleCorrect        = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true) // Green
	styleIncorrect      = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)  // Red
	stylePartial        = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true) // Yellow
	styleHighlight      = lipgloss.NewStyle().Background(lipgloss.Color("22")).Foreground(lipgloss.Color("0"))
	styleClitic         = lipgloss.NewStyle().Foreground(lipgloss.Color("13")) // Pink/Magenta
	styleSubtle         = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleHeader         = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	styleError          = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Padding(1)
	styleInputDiff      = lipgloss.NewStyle().Background(lipgloss.Color("9")).Foreground(lipgloss.Color("0"))
	styleCorrectDiff    = lipgloss.NewStyle().Background(lipgloss.Color("10")).Foreground(lipgloss.Color("0"))
	styleScenarioYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // Yellow for scenario name in-game
	styleCursor         = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	styleBarGreen       = lipgloss.NewStyle().Background(lipgloss.Color("10")).SetString(" ")
	styleBarRed         = lipgloss.NewStyle().Background(lipgloss.Color("9")).SetString(" ")
	wordSeparator       = " "
)

// --- DATA STRUCTURES ---

type Sentence struct {
	ID         int64
	Scenario   string
	Finnish    string
	English    string
	Words      []string
	CleanWords []string
}

type ScenarioStat struct {
	Name          string
	TotalPlays    int
	CorrectPlays  int
	SentencesInDB int
}

type gameState int

const (
	stateScenarioSelection gameState = iota
	statePlaying
	stateRoundOver
)

type wordAttemptData struct {
	WordIndex int
	UserInput string
	IsCorrect bool
	Duration  time.Duration
}

type WordAttemptDetail struct {
	WordIndex  int    `json:"wordIndex"`
	UserInput  string `json:"userInput"`
	IsCorrect  bool   `json:"isCorrect"`
	DurationMs int64  `json:"durationMs"`
}

type statsReloadedMsg struct {
	stats []ScenarioStat
	err   error
}

type model struct {
	db                   *sql.DB
	textInput            textinput.Model // This is for the game state
	filterInput          textinput.Model // New: for the scenario filter
	err                  error
	state                gameState
	allSentences         []Sentence
	sessionSentences     []Sentence
	roundResult          struct{ isCorrect bool }
	sentenceIdx          int
	wordIdx              int
	wordStartTime        time.Time
	roundAnalytics       []wordAttemptData
	scenarioStats        []ScenarioStat // All stats, sorted once
	filteredStats        []ScenarioStat // The stats currently visible after filtering
	selectedScenarios    map[string]bool
	cursor               int
	maxScenarioNameWidth int
	viewportStart        int // New: for scrolling
	viewportHeight       int // New: for scrolling
}

// --- CORE LOGIC & HELPERS ---

func cleanWord(s string) string {
	s = strings.ToLower(s)
	s = strings.Trim(s, `.,!?;:"()[]{}„“`)
	return s
}

func cipherWord(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case strings.ContainsRune("aouAOU", r):
			b.WriteRune('U')
		case strings.ContainsRune("eiEI", r):
			b.WriteRune('E')
		case strings.ContainsRune("äöyÄÖY", r):
			b.WriteRune('Ä')
		case strings.ContainsRune("bcdfghjklmnpqrstvwxzBCDFGHJKLMNPQRSTVWXZ", r):
			b.WriteRune('x')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func applyCliticStyling(word string) string {
	var styledClitics []string
	stem := word
	for {
		found := false
		for _, clitic := range CLITICS {
			if strings.HasSuffix(strings.ToLower(stem), clitic) {
				cliticPart := stem[len(stem)-len(clitic):]
				styledClitics = append([]string{styleClitic.Render(cliticPart)}, styledClitics...)
				stem = stem[:len(stem)-len(clitic)]
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	return stem + strings.Join(styledClitics, "")
}

func diffStrings(input, target string) (string, string) {
	var inputStyled, targetStyled strings.Builder
	runesInput := []rune(input)
	runesTarget := []rune(target)
	maxLen := len(runesInput)
	if len(runesTarget) > maxLen {
		maxLen = len(runesTarget)
	}
	for i := 0; i < maxLen; i++ {
		inputInBounds := i < len(runesInput)
		targetInBounds := i < len(runesTarget)
		if inputInBounds && targetInBounds {
			inputRune, targetRune := runesInput[i], runesTarget[i]
			if unicode.ToLower(inputRune) == unicode.ToLower(targetRune) {
				inputStyled.WriteString(string(inputRune))
				targetStyled.WriteString(string(targetRune))
			} else {
				inputStyled.WriteString(styleInputDiff.Render(string(inputRune)))
				targetStyled.WriteString(styleCorrectDiff.Render(string(targetRune)))
			}
		} else if inputInBounds {
			inputStyled.WriteString(styleInputDiff.Render(string(runesInput[i])))
		} else if targetInBounds {
			targetStyled.WriteString(styleCorrectDiff.Render(string(runesTarget[i])))
		}
	}
	return inputStyled.String(), targetStyled.String()
}

func (m *model) applyFilter() {
	filterText := strings.ToLower(m.filterInput.Value())
	m.filteredStats = []ScenarioStat{}
	for _, stat := range m.scenarioStats {
		if strings.Contains(strings.ToLower(stat.Name), filterText) {
			m.filteredStats = append(m.filteredStats, stat)
		}
	}
	// Reset cursor and viewport after filtering
	if m.cursor >= len(m.filteredStats) {
		m.cursor = 0
	}
	m.updateViewport()
}

// ADDED: This command fetches stats from the DB and sends a message when complete.
func reloadStatsCmd(db *sql.DB) tea.Cmd {
	return func() tea.Msg {
		stats, err := getScenarioStats(db)
		if err != nil {
			return statsReloadedMsg{err: err}
		}
		sortedStats := sortStats(stats) // Use our refactored sorting logic
		return statsReloadedMsg{stats: sortedStats}
	}
}

func (m *model) updateViewport() {
	if len(m.filteredStats) == 0 {
		m.viewportStart = 0
		return
	}

	// If cursor is above the viewport, move viewport up
	if m.cursor < m.viewportStart {
		m.viewportStart = m.cursor
	}

	// If cursor is below the viewport, move viewport down
	if m.cursor >= m.viewportStart+m.viewportHeight {
		m.viewportStart = m.cursor - m.viewportHeight + 1
	}
}

// --- BUBBLETEA IMPLEMENTATION ---

func newModel(db *sql.DB, sentences []Sentence, stats []ScenarioStat) model {
	// Game input
	ti := textinput.New()
	ti.Placeholder = "Type the word and press Enter..."
	ti.Focus()
	ti.CharLimit = 50
	ti.Width = 50
	ti.Prompt = ""

	// Filter input for scenario selection
	filterInput := textinput.New()
	filterInput.Placeholder = "Filter scenarios by name..."
	filterInput.Focus()
	filterInput.CharLimit = 50
	filterInput.Width = 50
	filterInput.Prompt = "> "

	maxWidth := 0
	for _, s := range stats {
		if len(s.Name) > maxWidth {
			maxWidth = len(s.Name)
		}
	}

	// Always start the cursor at the top of the list (index 0).
	// This provides a more conventional user experience and fixes the issue
	// where the top-played scenarios were not immediately visible.
	cursor := 0

	m := model{
		db:                   db,
		allSentences:         sentences,
		textInput:            ti,
		filterInput:          filterInput,
		state:                stateScenarioSelection,
		scenarioStats:        stats, // The full, sorted list
		filteredStats:        stats, // Initially, all stats are visible
		selectedScenarios:    make(map[string]bool),
		roundAnalytics:       make([]wordAttemptData, 0),
		maxScenarioNameWidth: maxWidth,
		cursor:               cursor,
		viewportHeight:       15, // How many items to show at once
	}

	m.updateViewport() // This will now correctly position the view at the top.
	return m
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateScenarioSelection:
		return m.updateScenarioSelection(msg)
	case statePlaying, stateRoundOver:
		return m.updatePlaying(msg)
	default:
		return m, nil
	}
}

// FIXED: Corrected the duplicate case for tea.KeyRunes

func (m *model) updateScenarioSelection(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	// ADDED: Handle the message from our command to refresh stats
	case statsReloadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.scenarioStats = msg.stats
		m.applyFilter() // Re-apply filter and reset cursor/viewport for the new stats
		return m, nil

	case tea.KeyMsg:
		// These keys are for navigation and actions, not for the text input.
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit

		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
				m.updateViewport()
			}
			return m, nil

		case tea.KeyDown:
			if m.cursor < len(m.filteredStats)-1 {
				m.cursor++
				m.updateViewport()
			}
			return m, nil

		case tea.KeyCtrlA:
			for _, stat := range m.filteredStats {
				m.selectedScenarios[stat.Name] = true
			}
			return m, nil

		case tea.KeyCtrlD:
			m.selectedScenarios = make(map[string]bool)
			return m, nil

		case tea.KeyTab:
			if len(m.filteredStats) > 0 {
				scenarioName := m.filteredStats[m.cursor].Name
				m.selectedScenarios[scenarioName] = !m.selectedScenarios[scenarioName]
				if m.cursor < len(m.filteredStats)-1 {
					m.cursor++
					m.updateViewport()
				}
			}
			return m, nil

		case tea.KeyShiftTab:
			if len(m.filteredStats) > 0 {
				scenarioName := m.filteredStats[m.cursor].Name
				m.selectedScenarios[scenarioName] = !m.selectedScenarios[scenarioName]
				if m.cursor > 0 {
					m.cursor--
					m.updateViewport()
				}
			}
			return m, nil

		case tea.KeyEnter:
			m.sessionSentences = []Sentence{}
			for _, s := range m.allSentences {
				if m.selectedScenarios[s.Scenario] {
					m.sessionSentences = append(m.sessionSentences, s)
				}
			}

			if len(m.sessionSentences) > 0 {
				rand.Shuffle(len(m.sessionSentences), func(i, j int) {
					m.sessionSentences[i], m.sessionSentences[j] = m.sessionSentences[j], m.sessionSentences[i]
				})
				m.state = statePlaying
				m.sentenceIdx = 0
				m.wordIdx = 0
				m.roundAnalytics = make([]wordAttemptData, 0)
				m.wordStartTime = time.Now()
				m.textInput.Focus() // Switch focus to game input
				m.textInput.SetValue("")
			}
			return m, nil
		}
	}

	// Pass all other messages to the filter text input.
	oldFilter := m.filterInput.Value()
	m.filterInput, cmd = m.filterInput.Update(msg)

	// If the filter text changed, re-apply the filter.
	if m.filterInput.Value() != oldFilter {
		m.applyFilter()
	}

	return m, cmd
}

func (m *model) updatePlaying(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit

		// MODIFIED: Esc returns to menu and triggers a stat reload.
		case tea.KeyEsc:
			m.state = stateScenarioSelection
			m.filterInput.Focus() // Give focus back to the filter input
			return m, reloadStatsCmd(m.db)

		case tea.KeyEnter:
			if m.state == stateRoundOver {
				m.state = statePlaying
				m.wordIdx = 0
				m.sentenceIdx = (m.sentenceIdx + 1) % len(m.sessionSentences)
				m.textInput.SetValue("")
				m.roundAnalytics = make([]wordAttemptData, 0)
				m.wordStartTime = time.Now()
				return m, nil
			}

			currentSentence := m.sessionSentences[m.sentenceIdx]
			targetWord := currentSentence.CleanWords[m.wordIdx]
			userInput := cleanWord(m.textInput.Value())
			isCorrect := (userInput == targetWord)
			duration := time.Since(m.wordStartTime)

			attempt := wordAttemptData{
				WordIndex: m.wordIdx,
				UserInput: m.textInput.Value(),
				IsCorrect: isCorrect,
				Duration:  duration,
			}
			m.roundAnalytics = append(m.roundAnalytics, attempt)
			m.roundResult.isCorrect = isCorrect

			logPlay(m.db, currentSentence.ID, isCorrect)

			if isCorrect {
				m.wordIdx++
				m.textInput.SetValue("")
				m.wordStartTime = time.Now()
				if m.wordIdx >= len(currentSentence.Words) {
					m.state = stateRoundOver
					logSentenceResult(m.db, currentSentence.ID, true, m.roundAnalytics)
				}
			} else {
				m.state = stateRoundOver
				logSentenceResult(m.db, currentSentence.ID, false, m.roundAnalytics)
			}
			return m, nil
		}
	case error:
		m.err = msg
		return m, nil
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.err != nil {
		return styleError.Render("Error: " + m.err.Error())
	}
	switch m.state {
	case stateScenarioSelection:
		return m.viewScenarioSelection()
	case stateRoundOver:
		return m.viewRoundOver()
	case statePlaying:
		if len(m.sessionSentences) == 0 {
			return m.viewScenarioSelection()
		}
		return m.viewPlaying()
	default:
		return "Unknown state."
	}
}

func (m *model) viewScenarioSelection() string {
	var b strings.Builder
	b.WriteString(styleHeader.Render("finyap-go: Scenario Selection"))
	b.WriteString("\n\n")
	b.WriteString(m.filterInput.View())
	b.WriteString("\n\n")

	// Calculate viewport boundaries
	start := m.viewportStart
	end := m.viewportStart + m.viewportHeight
	if end > len(m.filteredStats) {
		end = len(m.filteredStats)
	}

	if len(m.filteredStats) == 0 {
		b.WriteString("No scenarios match your filter.\n")
	} else {
		// Render only the visible part of the list
		format := fmt.Sprintf("%%s %%s %%-%ds | Plays: %%-5d | %%s %%.0f%%%%", m.maxScenarioNameWidth) // <- REMOVED TRAILING \n
		for i := start; i < end; i++ {
			stat := m.filteredStats[i]
			cursor := " "
			if m.cursor == i {
				cursor = styleCursor.Render(">")
			}

			checked := "[ ]"
			if m.selectedScenarios[stat.Name] {
				checked = styleCorrect.Render("[x]")
			}

			var percentage float64
			if stat.TotalPlays > 0 {
				percentage = float64(stat.CorrectPlays) / float64(stat.TotalPlays) * 100
			}
			bar := renderBar(percentage/100, 40) // renderBar expects 0.0-1.0

			line := fmt.Sprintf(format, cursor, checked, stat.Name, stat.TotalPlays, bar, percentage)

			// Highlight the entire line if it's the current cursor position
			if m.cursor == i {
				b.WriteString(styleHighlight.Render(line))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n") // <- ADDED NEWLINE HERE, OUTSIDE THE RENDER
		}
	}

	b.WriteString(fmt.Sprintf("\n  %s", styleSubtle.Render(fmt.Sprintf("Showing %d of %d scenarios", len(m.filteredStats), len(m.scenarioStats)))))
	b.WriteString(styleSubtle.Render("\n\n ↑/↓: Navigate | tab: Toggle | enter: Start"))
	b.WriteString(styleSubtle.Render("\n ctrl+a: Select All (Filtered) | ctrl+d: Deselect All | esc: Quit"))
	return b.String()
}

func renderBar(percentage float64, width int) string {
	greenCount := int(percentage * float64(width))
	redCount := width - greenCount
	return strings.Repeat(styleBarGreen.String(), greenCount) +
		strings.Repeat(styleBarRed.String(), redCount)
}

func (m model) viewPlaying() string {
	var b strings.Builder
	const indent = "  "
	b.WriteString(styleHeader.Render("finyap-go"))
	b.WriteRune('\n')
	currentSentence := m.sessionSentences[m.sentenceIdx]
	b.WriteString(fmt.Sprintf("Scenario: %s [%d/%d]",
		styleScenarioYellow.Render(currentSentence.Scenario), m.sentenceIdx+1, len(m.sessionSentences)))
	b.WriteRune('\n')
	b.WriteString(currentSentence.English)
	b.WriteRune('\n')
	b.WriteRune('\n')
	var displayedWords []string
	for i, word := range currentSentence.Words {
		if i < m.wordIdx {
			displayedWords = append(displayedWords, styleCorrect.Render(applyCliticStyling(word)))
		} else if i == m.wordIdx {
			styledWord := applyCliticStyling(cipherWord(word))
			displayedWords = append(displayedWords, styleHighlight.Render(styledWord))
		} else {
			displayedWords = append(displayedWords, applyCliticStyling(cipherWord(word)))
		}
	}
	b.WriteString(indent)
	b.WriteString(strings.Join(displayedWords, wordSeparator))
	b.WriteRune('\n')
	b.WriteRune('\n')
	var promptPadding string
	if m.wordIdx > 0 {
		prefixSlice := displayedWords[:m.wordIdx]
		prefixString := strings.Join(prefixSlice, wordSeparator)
		prefixWidth := lipgloss.Width(prefixString) + lipgloss.Width(wordSeparator)
		promptPadding = strings.Repeat(" ", prefixWidth)
	}
	b.WriteString(indent)
	b.WriteString(promptPadding)
	b.WriteString(m.textInput.View())
	b.WriteRune('\n')
	feedbackLine := renderLiveFeedback(m.textInput.Value(), currentSentence.CleanWords[m.wordIdx])
	if feedbackLine != "" {
		b.WriteString(indent)
		b.WriteString(promptPadding)
		b.WriteString(feedbackLine)
		b.WriteRune('\n')
	}
	b.WriteRune('\n')
	b.WriteString(styleSubtle.Render("Press Esc or Ctrl+C to quit."))
	return b.String()
}

func (m model) viewRoundOver() string {
	var b strings.Builder
	currentSentence := m.sessionSentences[m.sentenceIdx]
	b.WriteString(styleHeader.Render("Round Over"))
	b.WriteRune('\n')
	if m.roundResult.isCorrect {
		b.WriteString(styleCorrect.Render("🎉 Correct! You completed the sentence."))
	} else {
		userInput := m.textInput.Value()
		targetWord := currentSentence.Words[m.wordIdx]
		styledInput, styledTarget := diffStrings(userInput, targetWord)
		b.WriteString(styleIncorrect.Render("❌ Not quite."))
		b.WriteString(fmt.Sprintf("\nYour input:   %s", styledInput))
		b.WriteString(fmt.Sprintf("\nCorrect word: %s", styledTarget))
	}
	b.WriteString("\n\nFull sentence:\n")
	b.WriteString(fmt.Sprintf("FI: %s\n", styleCorrect.Render(currentSentence.Finnish)))
	b.WriteString(fmt.Sprintf("EN: %s\n", currentSentence.English))
	b.WriteString(styleSubtle.Render("\nPress Enter to continue to the next sentence..."))
	return b.String()
}

func renderLiveFeedback(input, target string) string {
	input = cleanWord(input)
	if input == "" {
		return ""
	}

	// Convert strings to rune slices to handle multi-byte characters correctly
	inputRunes := []rune(input)
	targetRunes := []rune(target)

	var coloredChars []string

	for i, r := range inputRunes {
		if i >= len(targetRunes) {
			// Character is past the end of the target word
			coloredChars = append(coloredChars, styleIncorrect.Render(string(r)))
			continue
		}

		if r == targetRunes[i] {
			// The rune at this position matches
			coloredChars = append(coloredChars, styleCorrect.Render(string(r)))
		} else {
			// Mismatch
			coloredChars = append(coloredChars, styleIncorrect.Render(string(r)))
		}
	}
	return "Feedback: " + strings.Join(coloredChars, "")
}

// --- DATABASE FUNCTIONS ---

func initDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	createSentencesTableSQL := `
	CREATE TABLE IF NOT EXISTS sentences (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		scenario TEXT NOT NULL,
		finnish TEXT NOT NULL UNIQUE,
		english TEXT NOT NULL
	);`
	createPlaysTableSQL := `
	CREATE TABLE IF NOT EXISTS plays (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sentence_id INTEGER NOT NULL,
		was_correct BOOLEAN NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (sentence_id) REFERENCES sentences (id)
	);`
	createSentenceResultsTableSQL := `
	CREATE TABLE IF NOT EXISTS sentence_results (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sentence_id INTEGER NOT NULL,
		completed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		total_duration_ms INTEGER NOT NULL,
		was_successful BOOLEAN NOT NULL,
		attempt_details TEXT,
		FOREIGN KEY (sentence_id) REFERENCES sentences (id)
	);`
	for _, stmt := range []string{createSentencesTableSQL, createPlaysTableSQL, createSentenceResultsTableSQL} {
		if _, err := db.Exec(stmt); err != nil {
			return nil, err
		}
	}
	return db, nil
}

func syncSentencesWithDB(db *sql.DB, sentences *[]Sentence) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare("INSERT OR IGNORE INTO sentences (scenario, finnish, english) VALUES (?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, s := range *sentences {
		if _, err := stmt.Exec(s.Scenario, s.Finnish, s.English); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for i := range *sentences {
		s := &(*sentences)[i]
		err := db.QueryRow("SELECT id FROM sentences WHERE finnish = ?", s.Finnish).Scan(&s.ID)
		if err != nil {
			return fmt.Errorf("failed to get ID for sentence '%s': %w", s.Finnish, err)
		}
	}
	return nil
}

func logPlay(db *sql.DB, sentenceID int64, wasCorrect bool) {
	_, err := db.Exec("INSERT INTO plays (sentence_id, was_correct) VALUES (?, ?)", sentenceID, wasCorrect)
	if err != nil {
		log.Printf("Error logging play to DB: %v", err)
	}
}

func logSentenceResult(db *sql.DB, sentenceID int64, wasSuccessful bool, attempts []wordAttemptData) {
	var totalDuration time.Duration
	details := make([]WordAttemptDetail, len(attempts))
	for i, attempt := range attempts {
		totalDuration += attempt.Duration
		details[i] = WordAttemptDetail{
			WordIndex:  attempt.WordIndex,
			UserInput:  attempt.UserInput,
			IsCorrect:  attempt.IsCorrect,
			DurationMs: attempt.Duration.Milliseconds(),
		}
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		log.Printf("Error marshalling sentence result details to JSON: %v", err)
		return
	}
	_, err = db.Exec(
		"INSERT INTO sentence_results (sentence_id, was_successful, total_duration_ms, attempt_details) VALUES (?, ?, ?, ?)",
		sentenceID,
		wasSuccessful,
		totalDuration.Milliseconds(),
		string(detailsJSON),
	)
	if err != nil {
		log.Printf("Error logging sentence result to DB: %v", err)
	}
}

func getScenarioStats(db *sql.DB) ([]ScenarioStat, error) {
	query := `
		SELECT
			s.scenario,
			COUNT(sr.id) as total_plays,
			SUM(CASE WHEN sr.was_successful = 1 THEN 1 ELSE 0 END) as correct_plays,
			COUNT(DISTINCT s.id) as sentences_in_db
		FROM sentences s
		LEFT JOIN sentence_results sr ON s.id = sr.sentence_id
		GROUP BY s.scenario
		ORDER BY s.scenario ASC;
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query scenario stats: %w", err)
	}
	defer rows.Close()
	var stats []ScenarioStat
	for rows.Next() {
		var stat ScenarioStat
		var correctPlays sql.NullInt64
		if err := rows.Scan(&stat.Name, &stat.TotalPlays, &correctPlays, &stat.SentencesInDB); err != nil {
			return nil, fmt.Errorf("failed to scan scenario stat row: %w", err)
		}
		stat.CorrectPlays = int(correctPlays.Int64)
		stats = append(stats, stat)
	}
	return stats, nil
}

// --- DATA LOADING ---

func loadSentencesFromTSV() ([]Sentence, error) {
	var allSentences []Sentence
	err := filepath.WalkDir(scenariosDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".tsv") {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				log.Printf("Error reading file %s: %v", path, readErr)
				return nil
			}
			lines := strings.Split(string(content), "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) == "" {
					continue
				}
				parts := strings.SplitN(line, "\t", 2)
				if len(parts) != 2 {
					continue
				}
				finnishSentence := strings.TrimSpace(parts[0])
				words := strings.Fields(finnishSentence)
				if len(words) == 0 {
					continue
				}
				cleanWords := make([]string, len(words))
				for i, w := range words {
					cleanWords[i] = cleanWord(w)
				}
				allSentences = append(allSentences, Sentence{
					Scenario:   filepath.Base(path),
					Finnish:    finnishSentence,
					English:    strings.TrimSpace(parts[1]),
					Words:      words,
					CleanWords: cleanWords,
				})
			}
		}
		return nil
	})
	return allSentences, err
}

// --- MAIN FUNCTION ---

func sortStats(stats []ScenarioStat) []ScenarioStat {
	groupedStats := make(map[int][]ScenarioStat)
	for _, s := range stats {
		groupedStats[s.TotalPlays] = append(groupedStats[s.TotalPlays], s)
	}

	var playCounts []int
	for pc := range groupedStats {
		playCounts = append(playCounts, pc)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(playCounts)))

	var sortedAndShuffledStats []ScenarioStat
	for _, pc := range playCounts {
		group := groupedStats[pc]
		rand.Shuffle(len(group), func(i, j int) {
			group[i], group[j] = group[j], group[i]
		})
		sortedAndShuffledStats = append(sortedAndShuffledStats, group...)
	}
	return sortedAndShuffledStats
}

func main() {
	rand.Seed(time.Now().UnixNano())
	sentences, err := loadSentencesFromTSV()
	if err != nil {
		log.Fatalf("Failed to load scenario files: %v", err)
	}
	db, err := initDB()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()
	if err := syncSentencesWithDB(db, &sentences); err != nil {
		log.Fatalf("Failed to sync sentences with database: %v", err)
	}

	stats, err := getScenarioStats(db)
	if err != nil {
		log.Fatalf("Failed to get scenario stats: %v", err)
	}

	if len(sentences) == 0 && len(stats) == 0 {
		fmt.Printf("No sentences found in '%s' directory. Exiting.\n", scenariosDir)
		os.Exit(0)
	}

	// Initialize the program, ensuring the stats are sorted by play count before being passed to the model.
	// This removes the intermediate `sortedStats` variable to prevent potential mix-ups.
	p := tea.NewProgram(newModel(db, sentences, sortStats(stats)))
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
	}
}
