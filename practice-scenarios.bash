#!/bin/bash

# Find all TSV files and count them
all_tsv_files=$(find scenarios/ -name "*.tsv")
total_tsv_files=$(echo "$all_tsv_files" | wc -l)
current_tsv_index=0

echo "$all_tsv_files" | shuf | while read -r file; do
  current_tsv_index=$((current_tsv_index + 1))
  for i in {1..10}; do
    clear

    echo "### [${current_tsv_index}/${total_tsv_files}] ${file}" # Added header
    echo "### [${i}/10] bash finyap.bash --input $file "
    echo ""
    echo ""
    echo ""
    bash finyap.bash --input "$file"

    echo ""
    echo ""
    echo ""
    echo ""
    echo ""

    read -p "Review your answer. Press Enter to continue, or type 'q' to quit. " user_input </dev/tty

    if [[ -z "$user_input" ]]; then
      continue
    elif [[ "$user_input" == "q" || "$user_input" == "Q" ]]; then
      echo "Exiting."
      exit 0
    fi
  done
done

echo "All scenarios processed."
exit 0
