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
	"strconv" // ADDED: For parsing sentence count
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

// MODIFIED: Added a new initial state for configuration.
type gameState int

const (
	stateSentenceCountInput gameState = iota // New: Ask for sentence count first
	stateScenarioSelection
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

// MODIFIED: model struct now includes state for the new input screen.
type model struct {
	db                   *sql.DB
	textInput            textinput.Model // This is for the game state
	filterInput          textinput.Model // For the scenario filter
	sentenceCountInput   textinput.Model // New: For sentence count per scenario
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
	viewportStart        int
	viewportHeight       int
	sentencesPerScenario int // New: Stores the number of sentences to play per scenario
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

// MODIFIED: Ciphers a word and styles the ciphered portion corresponding to a clitic.
func cipherWordWithClitics(word string) string {
	var foundClitics []string
	stem := word

	// First, peel off any clitics from the end of the real word, right-to-left.
	for {
		found := false
		for _, clitic := range CLITICS {
			if strings.HasSuffix(strings.ToLower(stem), clitic) {
				cliticPart := stem[len(stem)-len(clitic):]
				foundClitics = append([]string{cliticPart}, foundClitics...)
				stem = stem[:len(stem)-len(clitic)]
				found = true
				break
			}
		}
		if !found {
			break
		}
	}

	// Cipher the remaining stem of the word.
	cipheredStem := cipherWord(stem)

	// Cipher and then style the clitics that were found.
	var styledClitics []string
	for _, clitic := range foundClitics {
		cipheredClitic := cipherWord(clitic)
		styledClitics = append(styledClitics, styleClitic.Render(cipheredClitic))
	}

	// Combine the ciphered stem with the ciphered, styled clitics.
	return cipheredStem + strings.Join(styledClitics, "")
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

func reloadStatsCmd(db *sql.DB) tea.Cmd {
	return func() tea.Msg {
		stats, err := getScenarioStats(db)
		if err != nil {
			return statsReloadedMsg{err: err}
		}
		sortedStats := sortStats(stats)
		return statsReloadedMsg{stats: sortedStats}
	}
}

func (m *model) updateViewport() {
	if len(m.filteredStats) == 0 {
		m.viewportStart = 0
		return
	}

	if m.cursor < m.viewportStart {
		m.viewportStart = m.cursor
	}

	if m.cursor >= m.viewportStart+m.viewportHeight {
		m.viewportStart = m.cursor - m.viewportHeight + 1
	}
}

// --- BUBBLETEA IMPLEMENTATION ---

// MODIFIED: newModel now initializes the sentence count input and starts in that state.
func newModel(db *sql.DB, sentences []Sentence, stats []ScenarioStat) model {
	// Game input
	ti := textinput.New()
	ti.Placeholder = "Type the word and press Enter..."
	ti.CharLimit = 50
	ti.Width = 50
	ti.Prompt = ""

	// Filter input for scenario selection
	filterInput := textinput.New()
	filterInput.Placeholder = "Filter scenarios by name..."
	filterInput.CharLimit = 50
	filterInput.Width = 50
	filterInput.Prompt = "> "

	// New: Sentence count input
	sentenceCountInput := textinput.New()
	sentenceCountInput.Placeholder = "10"
	sentenceCountInput.Prompt = "How many sentences per scenario? (default 10): "
	sentenceCountInput.Focus() // Start with this input focused
	sentenceCountInput.CharLimit = 3
	sentenceCountInput.Width = 10
	sentenceCountInput.SetValue("10")

	maxWidth := 0
	for _, s := range stats {
		if len(s.Name) > maxWidth {
			maxWidth = len(s.Name)
		}
	}

	cursor := 0

	m := model{
		db:                   db,
		allSentences:         sentences,
		textInput:            ti,
		filterInput:          filterInput,
		sentenceCountInput:   sentenceCountInput,      // Add new field
		state:                stateSentenceCountInput, // Set initial state
		scenarioStats:        stats,
		filteredStats:        stats,
		selectedScenarios:    make(map[string]bool),
		roundAnalytics:       make([]wordAttemptData, 0),
		maxScenarioNameWidth: maxWidth,
		cursor:               cursor,
		viewportHeight:       15,
	}

	m.updateViewport()
	return m
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

// MODIFIED: The main Update function now routes to a handler for the new state.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateSentenceCountInput:
		return m.updateSentenceCountInput(msg)
	case stateScenarioSelection:
		return m.updateScenarioSelection(msg)
	case statePlaying, stateRoundOver:
		return m.updatePlaying(msg)
	default:
		return m, nil
	}
}

// ADDED: This new function handles all logic for the initial sentence count input screen.
func (m *model) updateSentenceCountInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit

		case tea.KeyEnter:
			val := m.sentenceCountInput.Value()
			if val == "" {
				val = "10" // Use default if empty
			}

			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				m.err = fmt.Errorf("invalid input: '%s'. Please enter a positive number", val)
				return m, nil // Return without changing state, error will be displayed
			}

			m.sentencesPerScenario = n
			m.err = nil // Clear any previous error
			m.state = stateScenarioSelection
			m.filterInput.Focus() // Now focus the scenario filter input
			return m, textinput.Blink
		}
	}

	m.sentenceCountInput, cmd = m.sentenceCountInput.Update(msg)
	return m, cmd
}

// MODIFIED: The Enter key logic is rewritten to create sequential, scenario-based sessions.
func (m *model) updateScenarioSelection(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case statsReloadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.scenarioStats = msg.stats
		m.applyFilter()
		return m, nil

	case tea.KeyMsg:
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

		// --- THIS ENTIRE CASE IS REWRITTEN ---
		case tea.KeyEnter:
			// Get selected scenarios in the order they are displayed in the full list
			var orderedSelectedScenarios []string
			for _, stat := range m.scenarioStats {
				if m.selectedScenarios[stat.Name] {
					orderedSelectedScenarios = append(orderedSelectedScenarios, stat.Name)
				}
			}

			if len(orderedSelectedScenarios) == 0 {
				return m, nil // Do nothing if no scenarios are selected
			}

			// Group all sentences by scenario for efficient lookup
			sentencesByScenario := make(map[string][]Sentence)
			for _, s := range m.allSentences {
				sentencesByScenario[s.Scenario] = append(sentencesByScenario[s.Scenario], s)
			}

			// Build the session list sequentially
			m.sessionSentences = []Sentence{}
			for _, scenarioName := range orderedSelectedScenarios {
				scenarioSentences := sentencesByScenario[scenarioName]
				if len(scenarioSentences) == 0 {
					continue
				}

				// Shuffle the sentences *within* the current scenario
				rand.Shuffle(len(scenarioSentences), func(i, j int) {
					scenarioSentences[i], scenarioSentences[j] = scenarioSentences[j], scenarioSentences[i]
				})

				// Determine how many sentences to take
				numToTake := m.sentencesPerScenario
				if numToTake > len(scenarioSentences) {
					numToTake = len(scenarioSentences)
				}

				// Add the selected sentences to the session queue
				m.sessionSentences = append(m.sessionSentences, scenarioSentences[:numToTake]...)
			}

			if len(m.sessionSentences) > 0 {
				m.state = statePlaying
				m.sentenceIdx = 0
				m.wordIdx = 0
				m.roundAnalytics = make([]wordAttemptData, 0)
				m.wordStartTime = time.Now()
				m.textInput.Focus() // Switch focus to game input
				m.textInput.SetValue("")
				return m, textinput.Blink
			}

			return m, nil // Stay on screen if no sentences ended up being queued
		}
	}

	oldFilter := m.filterInput.Value()
	m.filterInput, cmd = m.filterInput.Update(msg)

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

		case tea.KeyEsc:
			m.state = stateScenarioSelection
			m.filterInput.Focus()
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

// MODIFIED: The main View function now routes to a view for the new state.
func (m model) View() string {
	if m.err != nil && m.state != stateSentenceCountInput {
		// Only show the generic error for non-setup states
		// The setup view handles its own error display
		return styleError.Render("Error: " + m.err.Error())
	}
	switch m.state {
	case stateSentenceCountInput:
		return m.viewSentenceCountInput()
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

// ADDED: This new function renders the initial sentence count input screen.
func (m *model) viewSentenceCountInput() string {
	var b strings.Builder
	b.WriteString(styleHeader.Render("finyap-go: Setup"))
	b.WriteString("\n\n")

	// Display error if one exists from invalid input
	if m.err != nil {
		b.WriteString(styleError.Render(m.err.Error()))
		b.WriteString("\n\n")
	}

	b.WriteString(m.sentenceCountInput.View())
	b.WriteString("\n\n")
	b.WriteString(styleSubtle.Render("Enter the number of sentences to practice from each selected scenario.\nPress Enter to continue, or Esc to quit."))

	return b.String()
}

func (m *model) viewScenarioSelection() string {
	var b strings.Builder
	b.WriteString(styleHeader.Render("finyap-go: Scenario Selection"))
	b.WriteString("\n\n")
	b.WriteString(m.filterInput.View())
	b.WriteString("\n\n")

	start := m.viewportStart
	end := m.viewportStart + m.viewportHeight
	if end > len(m.filteredStats) {
		end = len(m.filteredStats)
	}

	if len(m.filteredStats) == 0 {
		b.WriteString("No scenarios match your filter.\n")
	} else {
		format := fmt.Sprintf("%%s %%s %%-%ds | Plays: %%-5d | %%s %%.0f%%%%", m.maxScenarioNameWidth)
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
			bar := renderBar(percentage/100, 40)

			line := fmt.Sprintf(format, cursor, checked, stat.Name, stat.TotalPlays, bar, percentage)

			if m.cursor == i {
				b.WriteString(styleHighlight.Render(line))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n")
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
	const indent = "  "
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
			// Correctly guessed words are fully revealed and styled. No change here.
			displayedWords = append(displayedWords, styleCorrect.Render(applyCliticStyling(word)))
		} else if i == m.wordIdx {
			// MODIFIED: Use the new function for the currently active word.
			styledWord := cipherWordWithClitics(word)
			displayedWords = append(displayedWords, styleHighlight.Render(styledWord))
		} else {
			// MODIFIED: Use the new function for upcoming words.
			displayedWords = append(displayedWords, cipherWordWithClitics(word))
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

	inputRunes := []rune(input)
	targetRunes := []rune(target)

	var coloredChars []string

	for i, r := range inputRunes {
		if i >= len(targetRunes) {
			coloredChars = append(coloredChars, styleIncorrect.Render(string(r)))
			continue
		}

		if r == targetRunes[i] {
			coloredChars = append(coloredChars, styleCorrect.Render(string(r)))
		} else {
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

	p := tea.NewProgram(newModel(db, sentences, sortStats(stats)))
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
	}
}
