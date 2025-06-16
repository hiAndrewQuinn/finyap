#!/bin/bash

# Word Guessing Game with fzf
# Takes sentences from a TSV file, masks ALL words, and lets the user
# guess them sequentially from left to right using fzf.
# This version highlights common Finnish clitics in pink and displays
# the Finnish flag on a perfect typed match.

# --- Configuration ---
SENTENCE_FILE='example-sentences.tsv'
SAMPLED_LINES_COUNT=100 # Number of lines to sample from the large file
FINYAP_VERSION="0.0.2"

# --- Help and Version Functions ---
show_help() {
  cat <<EOF
Usage: $(basename "$0") [options]

Word Guessing Game with fzf.

Takes sentences from a TSV file, masks all words, and lets the user
guess them sequentially from left to right using fzf.

Options:
  -h, --help      Show this help message and exit.
      --version   Show script version and exit.
  --input FILE    Use a specific TSV file for sentences, instead
                  of our default, >100,000 line Tatoeba file.
EOF
}

# --- Argument Parsing ---
while [[ $# -gt 0 ]]; do
  key="$1"
  case $key in
  -h | --help)
    show_help
    exit 0
    ;;
  --version)
    echo "$(basename "$0") version $FINYAP_VERSION"
    exit 0
    ;;
  --input)
    if [[ -n "$2" ]]; then
      SENTENCE_FILE="$2"
      shift # past argument
      shift # past value
    else
      echo "Error: --input option requires a file path." >&2
      exit 1
    fi
    ;;
  *)
    # Unknown options are ignored.
    shift
    ;;
  esac
done

# --- ANSI Colors ---
C_HIGHLIGHT=$'\033[42;30m'         # Black Text on Green Background
C_BG_HIGHLIGHT_PINK=$'\033[45;30m' # Black Text on Pink Background
C_BG_HIGHLIGHT_YELLOW=$'\033[43;30m'
C_RESET=$'\033[0m'
C_GREEN=$'\033[1;32m'
C_YELLOW=$'\033[1;33m'
C_RED=$'\033[1;31m'
C_PINK=$'\033[1;35m'
C_BLUE=$'\033[1;34m'

echo -e "${C_BLUE}finyap v${FINYAP_VERSION} - https://github.com/hiAndrewQuinn/finyap - https://finbug.xyz/ - https://andrew-quinn.me/${C_RESET}"

# --- Sanity Checks ---
if ! command -v fzf &>/dev/null; then
  echo "Error: fzf is not installed. Please install fzf to run this script."
  exit 1
fi
if ! command -v bash &>/dev/null; then
  echo "Error: bash is not found. This script requires bash for its preview pane."
  exit 1
fi
if [[ ! -f "$SENTENCE_FILE" ]]; then
  echo "Error: Sentence file '$SENTENCE_FILE' not found."
  echo "Please create it or change the SENTENCE_FILE variable in the script."
  exit 1
fi
if [[ ! -r "$SENTENCE_FILE" ]]; then
  echo "Error: Sentence file '$SENTENCE_FILE' is not readable."
  exit 1
fi

# --- Helper function to print the Finnish flag ---
print_finnish_flag() {
  local b='\e[48;2;0;47;108m'    # True Color Blue
  local w='\e[48;2;255;255;255m' # True Color White
  local r='\e[0m'                # Reset
  echo -e ""                     # Add a leading newline for spacing
  echo -e "${w}     ${b}   ${w}           ${r}"
  echo -e "${w}     ${b}   ${w}           ${r}"
  echo -e "${b}                   ${r}"
  echo -e "${w}     ${b}   ${w}           ${r}"
  echo -e "${w}     ${b}   ${w}           ${r}"
}

# --- Helper function to clean a word for matching ---
clean_word() {
  local word="$1"
  # Convert to lowercase
  word=$(echo "$word" | tr '[:upper:]' '[:lower:]')
  # Remove leading/trailing punctuation
  word=$(echo "$word" | sed -E 's/^[[:punct:].,!?;:]+|[[:punct:].,!?;:]+$//g')
  echo "$word"
}

# --- Helper function to apply the cipher to a word using sed ---
# This function intentionally ignores the « and » characters used for clitic marking.
cipher_word() {
  local word_to_cipher="$1"
  # Apply the character replacement rules:
  # 1. Low Vowels (a, o, u) -> U
  # 2. Mid Vowels (e, i) -> E
  # 3. High Vowels (ä, ö, y) -> Ä
  # 4. Consonants -> x
  # Other characters are passed through. Order matters.
  echo "$word_to_cipher" | sed \
    -e 's/[aouAOU]/U/g' \
    -e 's/[eiEI]/E/g' \
    -e 's/[äöyÄÖY]/Ä/g' \
    -e 's/[bcdfghjklmnpqrstvwxzBCDFGHJKLMNPQRSTVWXZ]/x/g'
}

# --- Helper function to mark clitics for later coloring ---
# This function wraps clitics with « and » characters. It can handle stacked clitics.
add_clitic_markers() {
  local word_to_process="$1"
  local temp_word="$word_to_process"
  local processed_clitics_part=""
  # List of clitics to check for, case-insensitively.
  local clitics_list=("kaan" "kään" "kin" "han" "hän" "ko" "kö" "pa" "pä")

  while true; do
    local found_in_pass=false
    for clitic in "${clitics_list[@]}"; do
      # Check if the end of the current word part matches a clitic (case-insensitively)
      if [[ "$(echo "$temp_word" | tr '[:upper:]' '[:lower:]')" == *"$clitic" ]]; then
        local suffix_len=${#clitic}
        local original_clitic_case="${temp_word: -$suffix_len}"
        if [[ "$(echo "$original_clitic_case" | tr '[:upper:]' '[:lower:]')" == "$clitic" ]]; then
          # It's a match. Prepend the marked clitic to our result string.
          processed_clitics_part="«${original_clitic_case}»${processed_clitics_part}"
          # And remove it from the word we're checking.
          temp_word="${temp_word::-$suffix_len}"
          found_in_pass=true
          break # Restart inner loop on the now-shortened word
        fi
      fi
    done
    # If we complete a full pass over the clitics list without a match, we're done.
    if ! $found_in_pass; then
      break
    fi
  done
  # Combine the remaining word stem with the marked clitics.
  echo "${temp_word}${processed_clitics_part}"
}

# --- Helper function for fzf preview (executed by bash -c) ---
run_fzf_preview() {
  local current_fzf_query="$1"
  local current_fzf_selection="$2"
  local query_for_comparison
  query_for_comparison=$(echo "$current_fzf_query" | tr '[:upper:]' '[:lower:]' | sed -E 's/^[[:punct:].,!?;:]+|[[:punct:].,!?;:]+$//g')
  local selection_for_comparison="$current_fzf_selection"

  # Use `echo -e` to correctly render the ANSI color codes in the masked sentence.
  echo -e "${C_BLUE}finyap v${FINYAP_VERSION} - https://github.com/hiAndrewQuinn/finyap - https://finbug.xyz/ - https://andrew-quinn.me/${C_RESET}"
  echo ""
  echo -e "Sentence file: ${C_YELLOW}${SENTENCE_FILE}${C_RESET}"
  echo -e "Sentence:      $FZF_PREVIEW_MASKED_SENTENCE"
  echo "English:       $FZF_PREVIEW_ENGLISH_TRANSLATION"
  echo ""

  if [[ "$FZF_PREVIEW_TARGET_WORD" == "$query_for_comparison" ]]; then
    echo "Typed so far:  ${C_GREEN}${query_for_comparison}${C_RESET}"
  elif [[ "$FZF_PREVIEW_TARGET_WORD" == "$query_for_comparison"* ]]; then
    echo "Typed so far:  ${C_YELLOW}${query_for_comparison}${C_RESET}"
  else
    echo "Typed so far:  ${C_RED}${query_for_comparison}${C_RESET}"
  fi

  if [[ -n "$selection_for_comparison" && "$selection_for_comparison" == "$FZF_PREVIEW_TARGET_WORD" ]]; then
    if [[ "$FZF_PREVIEW_TARGET_WORD" == "$query_for_comparison" ]]; then
      echo -e "\n\n${C_GREEN}Correct word selected! PERFECT TYPING!!${C_RESET}"
      print_finnish_flag # Display the flag on a perfect match!
    elif [[ "$FZF_PREVIEW_TARGET_WORD" == "$query_for_comparison"* ]]; then
      echo -e "\n\n${C_GREEN}Correct word selected! ${C_RESET}${C_YELLOW}You're getting there!!${C_RESET}"
      echo ""
      echo -e "\n${C_BG_HIGHLIGHT_YELLOW}¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥${C_RESET}"
      echo -e "${C_BG_HIGHLIGHT_YELLOW}¥¥¥${C_RESET}${C_YELLOW} Go for gold! Type it perfectly without looking down!! ${C_RESET}${C_BG_HIGHLIGHT_YELLOW}¥¥¥${C_RESET}    (Or press Enter mow to continue)"
      echo -e "${C_BG_HIGHLIGHT_YELLOW}¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥¥${C_RESET}"
    else
      echo -e "\n\n${C_GREEN}Correct word selected! ${C_RESET}${C_RED}But you've got the letters mixed up!!!${C_RESET}"
      echo ""
      echo -e "\n${C_BG_HIGHLIGHT_PINK}Slow down! Hit Backspace!! You can still win this!!!${C_RESET}"
    fi
  elif [[ -n "$current_fzf_query" ]]; then
    if [[ "$FZF_PREVIEW_TARGET_WORD" == "$query_for_comparison"* ]]; then
      if [[ "$query_for_comparison" == "$FZF_PREVIEW_TARGET_WORD" ]]; then
        echo -e "\n${C_GREEN}PERFECT typing!${C_RESET} (Press Enter if selected)"
        print_finnish_flag # Display the flag on a perfect match!
      else
        echo -e "\n${C_YELLOW}GOOD...${C_RESET} (typed: \"$current_fzf_query\")"
      fi
    else
      echo -e "\n${C_RED}BAD!${C_RESET} (typed: \"$current_fzf_query\")"
    fi
  else
    echo -e "\nStart typing, or use arrows to select the missing word..."
  fi

}
# Export the functions and variables for the fzf subshell
export -f run_fzf_preview print_finnish_flag
export C_GREEN C_YELLOW C_RED C_RESET C_PINK C_HIGHLIGHT C_BG_HIGHLIGHT_PINK C_BG_HIGHLIGHT_YELLOW SENTENCE_FILE FINYAP_VERSION C_BLUE

# --- 0. Pre-sample sentences from the large file ---
echo "Sampling at most $SAMPLED_LINES_COUNT random lines from $SENTENCE_FILE..."
sampled_data=$(shuf "$SENTENCE_FILE" | head -n "$SAMPLED_LINES_COUNT")

if [[ -z "$sampled_data" ]]; then
  echo "Error: Failed to sample any lines from '$SENTENCE_FILE'."
  unset -f run_fzf_preview print_finnish_flag # Clean up exported functions on error
  exit 1
fi
echo "Sampling complete. Preparing game..."

# --- 1. Prepare the list of unique Finnish words for fzf (FROM SAMPLED DATA) ---
all_finnish_words=$(echo "$sampled_data" | cut -f1 |
  tr -s '[:space:]' '\n' |
  while IFS= read -r word_token; do
    cleaned=$(clean_word "$word_token")
    if [[ -n "$cleaned" ]]; then
      echo "$cleaned"
    fi
  done |
  sort -u |
  grep -v '^$')

if [[ -z "$all_finnish_words" ]]; then
  echo "Error: Could not extract any unique Finnish words from the sampled data."
  unset -f run_fzf_preview print_finnish_flag
  exit 1
fi

# --- 2. Game Setup: Select a random sentence (FROM SAMPLED DATA) ---
random_line=$(echo "$sampled_data" | shuf -n 1)

if [[ -z "$random_line" ]]; then
  echo "Error: Failed to select a random line from the sampled data."
  unset -f run_fzf_preview print_finnish_flag
  exit 1
fi

finnish_sentence=$(echo "$random_line" | cut -f1)
english_translation=$(echo "$random_line" | cut -f2)

IFS=' ' read -r -a words_in_sentence <<<"$finnish_sentence"
words_in_sentence=(${words_in_sentence[@]}) # Re-evaluate to handle potential extra spaces

if [[ ${#words_in_sentence[@]} -eq 0 ]]; then
  echo "Error: Chosen Finnish sentence from sample is empty or could not be parsed."
  unset -f run_fzf_preview print_finnish_flag
  exit 1
fi

revealed_words=()
game_failed=false

# Export constant preview variables once
export FZF_PREVIEW_ENGLISH_TRANSLATION="$english_translation"

for i in "${!words_in_sentence[@]}"; do
  target_word_original="${words_in_sentence[$i]}"
  target_word_for_matching=$(clean_word "$target_word_original")

  # If a "word" is just punctuation or empty after cleaning, skip the guess.
  if [[ -z "$target_word_for_matching" ]]; then
    revealed_words+=("$target_word_original")
    continue
  fi

  # --- Construct the sentence for display/preview with clitic highlighting ---
  display_sentence_array=()
  # Part 1: Already revealed words (original form, with pink clitics)
  if [[ ${#revealed_words[@]} -gt 0 ]]; then
    processed_revealed=()
    for word in "${revealed_words[@]}"; do
      marked=$(add_clitic_markers "$word")
      colored=$(echo "$marked" | sed -e "s/«/${C_PINK}/g" -e "s/»/${C_RESET}/g")
      processed_revealed+=("$colored")
    done
    display_sentence_array+=("${processed_revealed[@]}")
  fi

  # Part 2: The current word to guess (highlighted, ciphered, with pink clitics)
  marked_current=$(add_clitic_markers "$target_word_original")
  ciphered_current=$(cipher_word "$marked_current")
  # For the active word, replace markers with special pink-on-green color
  # and reset back to the standard green highlight.
  colored_current=$(echo "$ciphered_current" | sed -e "s/«/${C_BG_HIGHLIGHT_PINK}/g" -e "s/»/${C_HIGHLIGHT}/g")
  display_sentence_array+=("${C_HIGHLIGHT}${colored_current}${C_RESET}")

  # Part 3: Future words (ciphered, with pink clitics)
  for ((j = i + 1; j < ${#words_in_sentence[@]}; j++)); do
    marked_future=$(add_clitic_markers "${words_in_sentence[j]}")
    ciphered_future=$(cipher_word "$marked_future")
    # For future words, use standard pink and reset to default text color.
    colored_future=$(echo "$ciphered_future" | sed -e "s/«/${C_PINK}/g" -e "s/»/${C_RESET}/g")
    display_sentence_array+=("$colored_future")
  done

  # Join the array into a string for display
  masked_sentence_for_display="${display_sentence_array[*]}"

  # Set variables for the fzf preview subshell for this loop iteration
  export FZF_PREVIEW_TARGET_WORD="$target_word_for_matching"
  export FZF_PREVIEW_MASKED_SENTENCE="$masked_sentence_for_display"

  # --- Run fzf ---
  start_time=$(date +%s.%N)
  selected_word_from_fzf=$(echo "$all_finnish_words" |
    fzf --ignore-case --layout=reverse --border \
      --prompt="  ${ciphered_current} " \
      --preview="bash -c 'run_fzf_preview \"\$1\" \"\$2\"' -- {q} {}" \
      --preview-window="up,65%,wrap,border-sharp")
  end_time=$(date +%s.%N)

  duration=$(awk -v s="$start_time" -v e="$end_time" 'BEGIN {print e-s}')
  duration_int=$(printf "%.0f" "$duration") # Integer part for comparison

  # Determine color based on time
  time_color="$C_GREEN"
  if [[ "$duration_int" -gt 10 ]]; then
    time_color="$C_RED"
  elif [[ "$duration_int" -gt 5 ]]; then
    time_color="$C_YELLOW"
  fi

  formatted_time=$(printf "(%.1fs)" "$duration")
  guess_time="${time_color}${formatted_time}${C_RESET}"

  # ... redone echo here. So that it looks like: [10.3] Hän pirtää xUxUU.
  echo -e "${guess_time} $masked_sentence_for_display"

  if [[ -z "$selected_word_from_fzf" ]]; then
    echo "${C_YELLOW}No word selected. Game aborted.${C_RESET}"
    game_failed=true
    break
  fi

  if [[ "$selected_word_from_fzf" == "$target_word_for_matching" ]]; then
    revealed_words+=("$target_word_original")
    # Color the correctly guessed word: stem is green (from context), clitic is pink.
    # We replace the end marker `»` with a simple RESET. The outer `echo` handles
    # the initial green color and the final reset for the whole line.
    colored_word=$(add_clitic_markers "$target_word_original" | sed -e "s/«/${C_PINK}/g" -e "s/»/${C_RESET}/g")
    # echo -e "${C_GREEN}Correct!${C_RESET} The word was: \"${C_GREEN}${colored_word}${C_RESET}\""
    # echo
  else
    echo -e "${C_RED}Not quite. Game over.${C_RESET}"
    echo "You selected: \"$selected_word_from_fzf\""
    echo "The correct word was: \"$target_word_original\""
    game_failed=true
    break
  fi
done

# --- 4. Final Game Summary ---
if [[ "$game_failed" == true ]]; then
  echo
  echo "The full sentence was:"
  echo "Finnish: $finnish_sentence"
  echo "English: $english_translation"
else
  echo -e "${C_GREEN}Congratulations! You completed the sentence!${C_RESET}"
  # For the final display, re-process the full sentence to show all clitics
  final_words_processed=()
  for word in "${words_in_sentence[@]}"; do
    marked=$(add_clitic_markers "$word")
    colored=$(echo "$marked" | sed -e "s/«/${C_PINK}/g" -e "s/»/${C_RESET}/g")
    final_words_processed+=("$colored")
  done
  echo -e "Finnish: ${final_words_processed[@]}"
  echo "English: $english_translation"
fi
echo

# Clean up exported variables and function
unset FZF_PREVIEW_TARGET_WORD FZF_PREVIEW_MASKED_SENTENCE FZF_PREVIEW_ENGLISH_TRANSLATION
unset C_GREEN C_YELLOW C_RED C_RESET C_PINK C_HIGHLIGHT C_BG_HIGHLIGHT_PINK
unset C_GREEN C_YELLOW C_RED C_RESET C_PINK C_HIGHLIGHT C_BG_HIGHLIGHT_PINK C_BG_HIGHLIGHT_YELLOW SENTENCE_FILE FINYAP_VERSION
unset -f run_fzf_preview print_finnish_flag

exit 0
