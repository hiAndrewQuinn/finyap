package main

import (
	"database/sql"
	"encoding/json" // NEW: Added for marshalling analytics data.
	"fmt"
	"io/fs"
	"log"
	"math/rand"
	"os"
	"path/filepath"
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

// CLITICS are suffixes that attach to words. We identify and style them separately.
var CLITICS = []string{"kaan", "kään", "kin", "han", "hän", "ko", "kö", "pa", "pä"}

// --- STYLING (using Lipgloss) ---

var (
	styleCorrect     = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)                      // Green
	styleIncorrect   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)                       // Red
	stylePartial     = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)                      // Yellow
	styleHighlight   = lipgloss.NewStyle().Background(lipgloss.Color("22")).Foreground(lipgloss.Color("0")) // Green background
	styleClitic      = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))                                 // Pink/Magenta
	styleSubtle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleHeader      = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	styleError       = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Padding(1)
	styleInputDiff   = lipgloss.NewStyle().Background(lipgloss.Color("9")).Foreground(lipgloss.Color("0"))  // Red BG
	styleCorrectDiff = lipgloss.NewStyle().Background(lipgloss.Color("10")).Foreground(lipgloss.Color("0")) // Green BG
	wordSeparator    = " "
)

// --- DATA STRUCTURES ---

// Sentence holds all data for a single Finnish/English sentence pair.
type Sentence struct {
	ID         int64
	Scenario   string
	Finnish    string
	English    string
	Words      []string
	CleanWords []string
}

// gameState represents the current state of the application UI.
type gameState int

const (
	statePlaying gameState = iota
	stateRoundOver
)

// NEW: wordAttemptData holds analytics for a single word attempt within a round.
type wordAttemptData struct {
	WordIndex int
	UserInput string // The raw user input for detailed error analysis.
	IsCorrect bool
	Duration  time.Duration
}

// NEW: WordAttemptDetail is the structure used for marshalling analytics data to JSON.
type WordAttemptDetail struct {
	WordIndex  int    `json:"wordIndex"`
	UserInput  string `json:"userInput"`
	IsCorrect  bool   `json:"isCorrect"`
	DurationMs int64  `json:"durationMs"`
}

// model is the core of our Bubbletea application, holding all state.
type model struct {
	db          *sql.DB
	sentences   []Sentence
	textInput   textinput.Model
	err         error
	state       gameState
	roundResult struct {
		isCorrect bool
	}

	// Game progression
	sentenceIdx int
	wordIdx     int

	// NEW: Analytics state for the current round.
	wordStartTime  time.Time
	roundAnalytics []wordAttemptData
}

// --- CORE LOGIC & HELPERS ---

// cleanWord converts a word to a lowercase, punctuation-free form for matching.
func cleanWord(s string) string {
	s = strings.ToLower(s)
	s = strings.Trim(s, `.,!?;:"()[]{}„“`)
	return s
}

// cipherWord applies the vowel/consonant mask to a word.
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
			// Pass through punctuation and other characters.
			b.WriteRune(r)
		}
	}
	return b.String()
}

// applyCliticStyling finds and styles known clitics in a word.
func applyCliticStyling(word string) string {
	var styledClitics []string
	stem := word

	// Iteratively strip clitics from the end of the word.
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

// diffStrings creates two styled strings showing a character-by-character comparison.
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
			inputRune := runesInput[i]
			targetRune := runesTarget[i]

			// Case-insensitive comparison
			if unicode.ToLower(inputRune) == unicode.ToLower(targetRune) {
				// Characters match (ignoring case), apply default styles.
				inputStyled.WriteString(string(inputRune))
				targetStyled.WriteString(string(targetRune))
			} else {
				// Characters are different, apply reverse video diff styles
				inputStyled.WriteString(styleInputDiff.Render(string(inputRune)))
				targetStyled.WriteString(styleCorrectDiff.Render(string(targetRune)))
			}
		} else if inputInBounds {
			// Input is longer, this is a mistake
			inputStyled.WriteString(styleInputDiff.Render(string(runesInput[i])))
		} else if targetInBounds {
			// Target is longer, this is a mistake
			targetStyled.WriteString(styleCorrectDiff.Render(string(runesTarget[i])))
		}
	}
	return inputStyled.String(), targetStyled.String()
}

// --- BUBBLETEA IMPLEMENTATION ---

// newModel initializes and returns the application's state model.
func newModel(db *sql.DB, sentences []Sentence) model {
	ti := textinput.New()
	ti.Placeholder = "Type the word and press Enter..."
	ti.Focus()
	ti.CharLimit = 50
	ti.Width = 50
	ti.Prompt = "" // We render the prompt manually for alignment.

	return model{
		db:             db,
		sentences:      sentences,
		textInput:      ti,
		state:          statePlaying,
		sentenceIdx:    0,
		wordIdx:        0,
		wordStartTime:  time.Now(),                 // NEW: Initialize timer for the very first word.
		roundAnalytics: make([]wordAttemptData, 0), // NEW: Initialize analytics slice.
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit

		case tea.KeyEnter:
			// --- STATE: Round is over, user wants to proceed ---
			if m.state == stateRoundOver {
				m.state = statePlaying
				m.wordIdx = 0
				m.sentenceIdx = (m.sentenceIdx + 1) % len(m.sentences) // Cycle through sentences
				m.textInput.SetValue("")

				// NEW: Reset analytics for the new round.
				m.roundAnalytics = make([]wordAttemptData, 0)
				m.wordStartTime = time.Now()

				return m, nil
			}

			// --- STATE: User is playing and submitted a word ---
			currentSentence := m.sentences[m.sentenceIdx]
			targetWord := currentSentence.CleanWords[m.wordIdx]
			userInput := cleanWord(m.textInput.Value())
			isCorrect := (userInput == targetWord)

			// NEW: Capture analytics for this specific word attempt.
			duration := time.Since(m.wordStartTime)
			attempt := wordAttemptData{
				WordIndex: m.wordIdx,
				UserInput: m.textInput.Value(), // Log raw input for detailed error review.
				IsCorrect: isCorrect,
				Duration:  duration,
			}
			m.roundAnalytics = append(m.roundAnalytics, attempt)

			m.roundResult.isCorrect = isCorrect // For the view.

			// Log the individual word attempt (old system, still useful for SR).
			logPlay(m.db, currentSentence.ID, isCorrect)

			if isCorrect {
				m.wordIdx++
				m.textInput.SetValue("")
				m.wordStartTime = time.Now() // Reset timer for the next word.

				// Check if the whole sentence is complete
				if m.wordIdx >= len(currentSentence.Words) {
					m.state = stateRoundOver // Transition to success screen
					// NEW: Log the full, successful sentence result to the database.
					logSentenceResult(m.db, currentSentence.ID, true, m.roundAnalytics)
				}
			} else {
				m.state = stateRoundOver // Transition to failure screen
				// NEW: Log the full, failed sentence result to the database.
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

	if len(m.sentences) == 0 {
		return styleError.Render("No sentences found in '" + scenariosDir + "'. Please add TSV files.")
	}

	if m.state == stateRoundOver {
		return m.viewRoundOver()
	}

	return m.viewPlaying()
}

// viewPlaying renders the main game screen.
func (m model) viewPlaying() string {
	var b strings.Builder
	const indent = "  " // Define the consistent indent

	b.WriteString(styleHeader.Render("finyap-go"))
	b.WriteRune('\n')

	currentSentence := m.sentences[m.sentenceIdx]

	b.WriteString(styleSubtle.Render(fmt.Sprintf("Scenario: %s [%d/%d]",
		currentSentence.Scenario, m.sentenceIdx+1, len(m.sentences))))
	b.WriteRune('\n')
	b.WriteString(currentSentence.English)
	b.WriteRune('\n')
	b.WriteRune('\n')

	// Render the sentence with cipher and highlights
	var displayedWords []string
	for i, word := range currentSentence.Words {
		if i < m.wordIdx {
			// Already guessed correctly
			displayedWords = append(displayedWords, styleCorrect.Render(applyCliticStyling(word)))
		} else if i == m.wordIdx {
			// The current word to guess
			styledWord := applyCliticStyling(cipherWord(word))
			displayedWords = append(displayedWords, styleHighlight.Render(styledWord))
		} else {
			// Future words
			displayedWords = append(displayedWords, applyCliticStyling(cipherWord(word)))
		}
	}
	b.WriteString(indent) // Apply indent
	b.WriteString(strings.Join(displayedWords, wordSeparator))
	b.WriteRune('\n')
	b.WriteRune('\n')

	// --- DYNAMIC ALIGNMENT LOGIC ---
	// Calculate the visual width of the sentence part before the current word.
	var promptPadding string
	if m.wordIdx > 0 {
		prefixSlice := displayedWords[:m.wordIdx]
		prefixString := strings.Join(prefixSlice, wordSeparator)
		prefixWidth := lipgloss.Width(prefixString) + lipgloss.Width(wordSeparator)
		promptPadding = strings.Repeat(" ", prefixWidth)
	}

	// Render the prompt and the text input view manually.
	b.WriteString(indent) // Apply indent
	b.WriteString(promptPadding)
	b.WriteString("")
	b.WriteString(m.textInput.View())
	b.WriteRune('\n')

	// Render the live feedback line, also padded for alignment.
	feedbackLine := renderLiveFeedback(m.textInput.Value(), currentSentence.CleanWords[m.wordIdx])
	if feedbackLine != "" {
		b.WriteString(indent) // Apply indent
		b.WriteString(promptPadding)
		b.WriteString(feedbackLine)
		b.WriteRune('\n')
	}

	b.WriteRune('\n')
	b.WriteString(styleSubtle.Render("Press Esc or Ctrl+C to quit."))

	return b.String()
}

// viewRoundOver renders the summary screen after a sentence is completed or failed.
func (m model) viewRoundOver() string {
	var b strings.Builder
	currentSentence := m.sentences[m.sentenceIdx]

	b.WriteString(styleHeader.Render("Round Over"))
	b.WriteRune('\n')

	if m.roundResult.isCorrect {
		b.WriteString(styleCorrect.Render("🎉 Correct! You completed the sentence."))
	} else {
		userInput := m.textInput.Value()
		targetWord := currentSentence.Words[m.wordIdx]

		// Generate the styled diffs
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

// renderLiveFeedback gives the user a colored string showing their typing accuracy in real-time.
func renderLiveFeedback(input, target string) string {
	input = cleanWord(input)
	if input == "" {
		return ""
	}

	var coloredChars []string
	for i, r := range input {
		if i >= len(target) {
			coloredChars = append(coloredChars, styleIncorrect.Render(string(r)))
			continue
		}
		if r == rune(target[i]) {
			coloredChars = append(coloredChars, styleCorrect.Render(string(r)))
		} else {
			coloredChars = append(coloredChars, styleIncorrect.Render(string(r)))
		}
	}

	return "Feedback: " + strings.Join(coloredChars, "")
}

// --- DATABASE FUNCTIONS ---

// initDB creates and opens a connection to the SQLite database.
func initDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// `sentences` stores the canonical list of sentences.
	createSentencesTableSQL := `
	CREATE TABLE IF NOT EXISTS sentences (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		scenario TEXT NOT NULL,
		finnish TEXT NOT NULL UNIQUE,
		english TEXT NOT NULL
	);`

	// `plays` logs every single attempt for granular spaced repetition analysis.
	createPlaysTableSQL := `
	CREATE TABLE IF NOT EXISTS plays (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sentence_id INTEGER NOT NULL,
		was_correct BOOLEAN NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (sentence_id) REFERENCES sentences (id)
	);`

	// NEW: `sentence_results` logs a summary of an entire sentence attempt,
	// including detailed performance data for deliberate practice analysis.
	createSentenceResultsTableSQL := `
	CREATE TABLE IF NOT EXISTS sentence_results (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sentence_id INTEGER NOT NULL,
		completed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		total_duration_ms INTEGER NOT NULL,
		was_successful BOOLEAN NOT NULL,
		attempt_details TEXT, -- This will be a JSON blob with word-by-word details.
		FOREIGN KEY (sentence_id) REFERENCES sentences (id)
	);`

	for _, stmt := range []string{createSentencesTableSQL, createPlaysTableSQL, createSentenceResultsTableSQL} {
		if _, err := db.Exec(stmt); err != nil {
			return nil, err
		}
	}

	return db, nil
}

// syncSentencesWithDB ensures all sentences from files are in the DB and retrieves their IDs.
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

	// Now fetch the IDs for all sentences
	for i := range *sentences {
		s := &(*sentences)[i]
		err := db.QueryRow("SELECT id FROM sentences WHERE finnish = ?", s.Finnish).Scan(&s.ID)
		if err != nil {
			return fmt.Errorf("failed to get ID for sentence '%s': %w", s.Finnish, err)
		}
	}

	return nil
}

// logPlay records a single guess attempt in the database.
func logPlay(db *sql.DB, sentenceID int64, wasCorrect bool) {
	_, err := db.Exec("INSERT INTO plays (sentence_id, was_correct) VALUES (?, ?)", sentenceID, wasCorrect)
	if err != nil {
		log.Printf("Error logging play to DB: %v", err)
	}
}

// NEW: logSentenceResult records the full analytics of a completed sentence round.
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

// --- DATA LOADING ---

// loadSentencesFromTSV walks the scenarios directory and parses all .tsv files.
func loadSentencesFromTSV() ([]Sentence, error) {
	var sentences []Sentence

	err := filepath.WalkDir(scenariosDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".tsv") {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			lines := strings.Split(string(content), "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) == "" {
					continue
				}
				parts := strings.SplitN(line, "\t", 2)
				if len(parts) != 2 {
					continue // Skip malformed lines
				}

				finnishSentence := strings.TrimSpace(parts[0])
				words := strings.Fields(finnishSentence)
				if len(words) == 0 {
					continue // Skip empty sentences
				}

				cleanWords := make([]string, len(words))
				for i, w := range words {
					cleanWords[i] = cleanWord(w)
				}

				sentences = append(sentences, Sentence{
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
	if err != nil {
		return nil, err
	}

	// Shuffle the sentences for variety
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(sentences), func(i, j int) {
		sentences[i], sentences[j] = sentences[j], sentences[i]
	})

	return sentences, nil
}

// --- MAIN FUNCTION ---

func main() {
	// 1. Load all sentences from files into memory
	sentences, err := loadSentencesFromTSV()
	if err != nil {
		log.Fatalf("Failed to load scenario files: %v", err)
	}

	if len(sentences) == 0 {
		fmt.Printf("No sentences found in '%s' directory. Exiting.\n", scenariosDir)
		os.Exit(0)
	}

	// 2. Initialize database and sync sentences
	db, err := initDB()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	if err := syncSentencesWithDB(db, &sentences); err != nil {
		log.Fatalf("Failed to sync sentences with database: %v", err)
	}

	// 3. Start the Bubbletea TUI program
	p := tea.NewProgram(newModel(db, sentences))
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
	}
}
