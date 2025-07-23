#!/bin/bash

# ==============================================================================
# finyap-practice.bash v1.1
# MODIFIED:
# 1. Scenarios are now copied to /dev/shm (RAM) for processing.
# 2. Quit command now exits the entire script globally.
# ==============================================================================

# --- ANSI Colors (from finyap.bash) ---
C_HIGHLIGHT=$'\033[42;30m'         # Black Text on Green Background
C_BG_HIGHLIGHT_PINK=$'\033[45;30m' # Black Text on Pink Background
C_BG_HIGHLIGHT_YELLOW=$'\033[43;30m'
C_RESET=$'\033[0m'
C_GREEN=$'\033[1;32m'
C_YELLOW=$'\033[1;33m'
C_RED=$'\033[1;31m'
C_PINK=$'\033[1;35m'
C_BLUE=$'\033[1;34m'
C_GREY=$'\033[2m'
FINYAP_VERSION="1.1-RAM-and-Global-Quit"

# --- HELPER FUNCTIONS (from finyap.bash) ---
# All helper functions are now defined globally once.

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
  echo ""
  echo -e '+----------------------------------------------------------------+'
  echo -e '|                                                                |'
  echo -e '| m    m   "      m           mmmmmm          m                  |'
  echo -e '| #    # mmm    mm#mm         #      m mm   mm#mm   mmm    m mm  |'
  echo -e '| #mmmm#   #      #           #mmmmm #"  #    #    #"  #   #"  " |'
  echo -e '| #    #   #      #           #      #   #    #    #""""   #     |'
  echo -e '| #    # mm#mm    "mm         #mmmmm #   #    "mm  "#mm"   #     |'
  echo -e '|                                                                |'
  echo -e '+----------------------------------------------------------------+'
}

clean_word() {
  local word="$1"
  word=$(echo "$word" | tr '[:upper:]' '[:lower:]')
  word=$(echo "$word" | sed -E 's/^[[:punct:].,!?;:]+|[[:punct:].,!?;:]+$//g')
  echo "$word"
}

cipher_word() {
  local word_to_cipher="$1"
  echo "$word_to_cipher" | sed \
    -e 's/[aouAOU]/U/g' \
    -e 's/[eiEI]/E/g' \
    -e 's/[äöyÄÖY]/Ä/g' \
    -e 's/[bcdfghjklmnpqrstvwxzBCDFGHJKLMNPQRSTVWXZ]/x/g'
}

add_clitic_markers() {
  local word_to_process="$1"
  local temp_word="$word_to_process"
  local processed_clitics_part=""
  local clitics_list=("kaan" "kään" "kin" "han" "hän" "ko" "kö" "pa" "pä")
  while true; do
    local found_in_pass=false
    for clitic in "${clitics_list[@]}"; do
      if [[ "$(echo "$temp_word" | tr '[:upper:]' '[:lower:]')" == *"$clitic" ]]; then
        local suffix_len=${#clitic}
        local original_clitic_case="${temp_word: -$suffix_len}"
        if [[ "$(echo "$original_clitic_case" | tr '[:upper:]' '[:lower:]')" == "$clitic" ]]; then
          processed_clitics_part="«${original_clitic_case}»${processed_clitics_part}"
          temp_word="${temp_word::-$suffix_len}"
          found_in_pass=true
          break
        fi
      fi
    done
    if ! $found_in_pass; then
      break
    fi
  done
  echo "${temp_word}${processed_clitics_part}"
}

run_fzf_preview() {
  local current_fzf_query="$1"
  local current_fzf_selection="$2"
  local query_for_comparison
  query_for_comparison=$(echo "$current_fzf_query" | tr '[:upper:]' '[:lower:]' | sed -E 's/^[[:punct:].,!?;:]+|[[:punct:].,!?;:]+$//g')
  local selection_for_comparison="$current_fzf_selection"

  echo -e "${C_BLUE}finyap v${FINYAP_VERSION} - $(date +%Y-%m-%d)${C_RESET}"
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
  echo ""
  echo -e "${C_GREY}Found a bug? Report it at https://github.com/hiAndrewQuinn/finyap/issues/new?labels=bug${C_RESET}"
  if [[ -n "$selection_for_comparison" && "$selection_for_comparison" == "$FZF_PREVIEW_TARGET_WORD" ]]; then
    if [[ "$FZF_PREVIEW_TARGET_WORD" == "$query_for_comparison" ]]; then
      echo -e "\n\n${C_GREEN}Correct word selected! PERFECT TYPING!!${C_RESET}"
      print_finnish_flag
    else
      echo -e "\n\n${C_GREEN}Correct word selected! Keep practicing your typing!${C_RESET}"
    fi
  fi
}

# Export functions and variables needed by the fzf preview subshell
export -f run_fzf_preview print_finnish_flag
export C_GREEN C_YELLOW C_RED C_RESET C_PINK C_HIGHLIGHT C_BG_HIGHLIGHT_PINK C_BG_HIGHLIGHT_YELLOW FINYAP_VERSION C_BLUE C_GREY

# --- MAIN GAME ROUND FUNCTION ---
# This function contains the logic from the main body of finyap.bash
run_game_round() {
  local scenario_file="$1"
  local current_round="$2"
  local total_rounds="$3"
  local all_finnish_words="$4" # Now passed as an argument
  local random_line="$5"       # Now passed as an argument

  clear
  echo "practice-scenarios: [${current_tsv_index}/${total_tsv_files}] ${scenario_file}"
  echo "practice-scenarios: [${current_round}/${total_rounds}]"
  echo ""
  echo "finyap v${FINYAP_VERSION} - https://github.com/hiAndrewQuinn/finyap - https://finbug.xyz/ - https://andrew-quinn.me/"
  echo "Found a bug? Report it at https://github.com/hiAndrewQuinn/finyap/issues/new?labels=bug"
  echo ""

  finnish_sentence=$(echo "$random_line" | cut -f1)
  english_translation=$(echo "$random_line" | cut -f2)

  IFS=' ' read -r -a words_in_sentence <<<"$finnish_sentence"
  if [[ ${#words_in_sentence[@]} -eq 0 ]]; then
    echo "Warning: Skipping empty sentence from '$scenario_file'."
    sleep 1
    return
  fi

  revealed_words=()
  game_failed=false
  export FZF_PREVIEW_ENGLISH_TRANSLATION="$english_translation"
  export SENTENCE_FILE="$scenario_file" # For preview display

  for i in "${!words_in_sentence[@]}"; do
    target_word_original="${words_in_sentence[$i]}"
    target_word_for_matching=$(clean_word "$target_word_original")

    if [[ -z "$target_word_for_matching" ]]; then
      revealed_words+=("$target_word_original")
      continue
    fi

    display_sentence_array=()
    if [[ ${#revealed_words[@]} -gt 0 ]]; then
      processed_revealed=()
      for word in "${revealed_words[@]}"; do
        marked=$(add_clitic_markers "$word")
        colored=$(echo "$marked" | sed -e "s/«/${C_PINK}/g" -e "s/»/${C_RESET}/g")
        processed_revealed+=("$colored")
      done
      display_sentence_array+=("${processed_revealed[@]}")
    fi

    marked_current=$(add_clitic_markers "$target_word_original")
    ciphered_current=$(cipher_word "$marked_current")
    colored_current=$(echo "$ciphered_current" | sed -e "s/«/${C_BG_HIGHLIGHT_PINK}/g" -e "s/»/${C_HIGHLIGHT}/g")
    display_sentence_array+=("${C_HIGHLIGHT}${colored_current}${C_RESET}")

    for ((j = i + 1; j < ${#words_in_sentence[@]}; j++)); do
      marked_future=$(add_clitic_markers "${words_in_sentence[j]}")
      ciphered_future=$(cipher_word "$marked_future")
      colored_future=$(echo "$ciphered_future" | sed -e "s/«/${C_PINK}/g" -e "s/»/${C_RESET}/g")
      display_sentence_array+=("$colored_future")
    done

    masked_sentence_for_display="${display_sentence_array[*]}"
    export FZF_PREVIEW_TARGET_WORD="$target_word_for_matching"
    export FZF_PREVIEW_MASKED_SENTENCE="$masked_sentence_for_display"

    start_time=$(date +%s.%N)
    selected_word_from_fzf=$(echo "$all_finnish_words" |
      fzf --ignore-case --layout=reverse --border \
        --prompt="   ${ciphered_current} " \
        --preview="bash -c 'run_fzf_preview \"\$1\" \"\$2\"' -- {q} {}" \
        --header-first \
        --preview-window="up,80%,wrap,border-sharp")

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

    formatted_time=$(printf "(%6.1fs)" "$duration")
    guess_time="${time_color}${formatted_time}${C_RESET}"

    # ... redone echo here. So that it looks like: [10.3] Hän pirtää xUxUU.
    echo -e "${guess_time} $masked_sentence_for_display    <-    ${time_color}${target_word_original}${C_RESET}"

    if [[ -z "$selected_word_from_fzf" ]]; then
      echo "${C_YELLOW}No word selected. Aborting this round.${C_RESET}"
      game_failed=true
      break
    fi

    if [[ "$selected_word_from_fzf" == "$target_word_for_matching" ]]; then
      revealed_words+=("$target_word_original")
    else
      echo
      echo -e "${C_RED}Not quite. Game over for this round.${C_RESET}"
      echo -e "You selected:         ${C_RED}${selected_word_from_fzf}${C_RESET}"
      echo -e "The correct word was: ${C_GREEN}${target_word_original}${C_RESET}"
      game_failed=true
      break
    fi
  done

  echo ""
  echo "============================================================"
  if [[ "$game_failed" == true ]]; then
    echo
  else
    echo -e "${C_GREEN}Congratulations! You completed the sentence!${C_RESET}"
  fi
  echo ""
  echo "The full sentence was:"
  echo "Finnish: $finnish_sentence"
  echo "English: $english_translation"
  echo "============================================================"
  echo ""

  # Add to check.csv logic
  echo "Review your answer."
  echo "- Press Enter to continue."
  echo "- Enter 'q' to (q)uit."
  echo "- Enter 'c' to save this sentence to check.csv."
  read -p "$ " user_input </dev/tty

  if [[ "$user_input" == "c" || "$user_input" == "C" ]]; then
    echo "\"$scenario_file\",\"$english_translation\",\"$finnish_sentence\"" >>check.csv
    echo "Entry saved to: $(realpath check.csv)"
    sleep 1
  elif [[ "$user_input" == "q"* || "$user_input" == "Q"* ]]; then
    echo "Exiting."
    # MODIFICATION 2.1: This exit command will now terminate the whole script
    # because the main loop no longer runs in a subshell.
    exit 0
  fi
}

# --- SCRIPT ENTRY POINT (from practice-scenarios.bash) ---

# Sanity check for fzf
if ! command -v fzf &>/dev/null; then
  echo "Error: fzf is not installed. It's required for file selection."
  exit 1
fi

# MODIFICATION 1.1: Add a trap to clean up temporary files on exit
trap 'rm -f /dev/shm/finyap_practice_*.tsv' EXIT

echo "============================================================"
echo " Finnish Yap Practice Scenarios (Refactored)"
echo "============================================================"
echo ""
read -p "Enter number of reviews per scenario [10]: " user_loop_count
loop_count=${user_loop_count:-10}

if ! [[ "$loop_count" =~ ^[0-9]+$ ]]; then
  echo "Invalid input. Defaulting to 10."
  loop_count=10
fi
echo ""

all_tsv_files=$(find scenarios/ -name "*.tsv" -type f | shuf)
total_available_files=$(echo "$all_tsv_files" | wc -l | xargs)

if [ "$total_available_files" -eq 0 ]; then
  echo "Error: No .tsv files found in the 'scenarios/' directory."
  exit 1
fi

echo "Found ${total_available_files} scenarios."
read -p "Process all of them? [Y/n]: " process_all
echo ""

files_to_process=""
if [[ -z "$process_all" || "$process_all" == "y" || "$process_all" == "Y" ]]; then
  files_to_process="$all_tsv_files"
else
  echo "Use TAB to select/deselect files, then press Enter to confirm."
  sleep 1
  files_to_process=$(echo "$all_tsv_files" | fzf \
    --multi --border --prompt="Select scenarios> " \
    --preview="cat {}")
fi

if [ -z "$files_to_process" ]; then
  echo "No files selected. Exiting."
  exit 0
fi

echo "$files_to_process" > so_far.txt
total_tsv_files=$(echo "$files_to_process" | wc -l | xargs)
current_tsv_index=0

if [ ! -f check.csv ]; then
  echo "File,English,Finnish" >check.csv
fi

# MODIFICATION 2.2: Change the main loop to use process substitution.
# This prevents the loop from running in a subshell, allowing `exit` to be global.
while IFS= read -r file; do
  current_tsv_index=$((current_tsv_index + 1))

  # MODIFICATION 1.2: Create a temporary copy of the scenario file in RAM (/dev/shm)
  temp_file="/dev/shm/finyap_practice_$(basename "$file")"
  cp "$file" "$temp_file"

  # --- MODIFIED SETUP ---
  # MODIFICATION 1.3: Use the in-memory temp_file for all operations
  echo "Preparing scenario: $file (processing from RAM)..."

  # 1. Build the word list from the ENTIRE scenario file for a complete fzf list.
  all_finnish_words=$(cut -f1 "$temp_file" | tr -s '[:space:]' '\n' |
    while IFS= read -r word_token; do
      cleaned=$(clean_word "$word_token")
      if [[ -n "$cleaned" ]]; then echo "$cleaned"; fi
    done | sort -u | grep -v '^$')

  # 2. Separately, get the specific lines we will actually play for this session.
  game_lines=$(shuf -n "$loop_count" "$temp_file")

  if [[ -z "$all_finnish_words" || -z "$game_lines" ]]; then
    echo "Warning: Could not extract words or sentences from '$file'. Skipping."
    # MODIFICATION 1.4: Clean up the temp file before continuing
    rm -f "$temp_file"
    sleep 2
    continue
  fi

  # Loop for the number of rounds, using the pre-sampled lines.
  round_num=0
  echo "$game_lines" | while read -r line_for_round; do
    round_num=$((round_num + 1))
    # Call the efficient game function with the full word list
    # We still pass the *original* filename for display purposes.
    run_game_round "$file" "$round_num" "$loop_count" "$all_finnish_words" "$line_for_round"
  done

  # MODIFICATION 1.5: Remove the temporary file from RAM after processing
  rm -f "$temp_file"

done < <(echo "$files_to_process")

echo "All selected scenarios processed."
exit 0
