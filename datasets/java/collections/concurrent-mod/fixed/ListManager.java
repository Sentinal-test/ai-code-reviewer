package com.example.service;

import java.util.ArrayList;
import java.util.Iterator;
import java.util.List;

public class ListManager {
    public void removeBadWords(List<String> words) {
        Iterator<String> iterator = words.iterator();
        while (iterator.hasNext()) {
            String word = iterator.next();
            if (word.startsWith("bad")) {
                iterator.remove();
            }
        }
        // Or words.removeIf(word -> word.startsWith("bad"));
    }

    public void processWords(List<String> words) {
        // ...
    }
}
