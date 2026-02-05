package com.example.service;

import java.util.ArrayList;
import java.util.List;

public class ListManager {
    public void removeBadWords(List<String> words) {
        for (String word : words) {
            if (word.startsWith("bad")) {
                words.remove(word);
            }
        }
    }

    public void processWords(List<String> words) {
        // ...
    }
}
