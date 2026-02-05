package com.example.service;

import com.example.model.User;
import com.example.repo.UserRepository;

public class UserService {
    private final UserRepository userRepo;

    public UserService(UserRepository userRepo) {
        this.userRepo = userRepo;
    }

    public String getUserDisplayName(String userId) {
        User user = userRepo.findById(userId);
        
        // userRepo.findById might return null if user not found.
        // Accessing user.getProfile() will throw NPE.
        return user.getProfile().getDisplayName();
    }

    public void updatedUser(String userId, String name) {
        User user = userRepo.findById(userId);
        if (user != null) {
            user.setName(name);
            userRepo.save(user);
        }
    }
}
