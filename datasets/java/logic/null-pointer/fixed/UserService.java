package com.example.service;

import com.example.model.User;
import com.example.repo.UserRepository;
import java.util.Optional;

public class UserService {
    private final UserRepository userRepo;

    public UserService(UserRepository userRepo) {
        this.userRepo = userRepo;
    }

    public String getUserDisplayName(String userId) {
        User user = userRepo.findById(userId);
        
        if (user == null || user.getProfile() == null) {
            return "Unknown User";
        }
        
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
