#!/bin/bash

# Exit on any error
set -e

# Get the directory where the script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
SOURCE_DIR="$SCRIPT_DIR/../model/best_model"
TARGET_DIR="$SCRIPT_DIR/models/best_model"

# Check if source directory exists
if [ ! -d "$SOURCE_DIR" ]; then
    echo "Error: Source directory $SOURCE_DIR does not exist"
    echo "Please train the model first by running the training script in the model directory"
    exit 1
fi

# Create the target directory if it doesn't exist
mkdir -p "$TARGET_DIR"

# Copy the model files
echo "Copying model files from $SOURCE_DIR to $TARGET_DIR"
cp -r "$SOURCE_DIR"/* "$TARGET_DIR"/

echo "Model files copied successfully" 