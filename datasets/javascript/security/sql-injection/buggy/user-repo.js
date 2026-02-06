const { db } = require('../database');

class UserRepository {
  constructor() {
    this.tableName = 'users';
  }

  async findUserByEmail(email) {
    // Simulate some logic
    if (!email) {
      throw new Error('Email is required');
    }

    const query = `SELECT * FROM ${this.tableName} WHERE email = "${email}"`;
    
    try {
      const result = await db.query(query);
      return result.rows[0];
    } catch (err) {
      console.error('Database error:', err);
      throw err;
    }
  }

  async getUserProfile(userId) {
    // Some other methods to make the file larger
    const query = 'SELECT * FROM profiles WHERE user_id = ?';
    const result = await db.query(query, [userId]);
    return result.rows[0];
  }

  // Helper methods...
  _formatUser(user) {
    return {
      id: user.id,
      name: user.name,
      email: user.email,
      createdAt: user.created_at
    };
  }
}

module.exports = UserRepository;
