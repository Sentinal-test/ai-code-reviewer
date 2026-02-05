const { Post, Author } = require('../models');

class PostController {
  async getAllPosts(req, res) {
    try {
      // FIXED: Use eager loading (JOIN) to fetch authors in a single query
      const posts = await Post.findAll({ 
        where: { published: true },
        include: [{ model: Author, attributes: ['name'] }]
      });
      
      const postsWithAuthors = posts.map(post => ({
        ...post.toJSON(),
        author: post.Author.name
      }));

      res.json(postsWithAuthors);
    } catch (error) {
      console.error(error);
      res.status(500).json({ error: 'Internal Server Error' });
    }
  }

  // Dummy methods to simulate larger file
  async getPostById(id) { /* ... */ }
  async createPost(data) { /* ... */ }
  async updatePost(id, data) { /* ... */ }
  async deletePost(id) { /* ... */ }
  // ... more dummy methods
}

module.exports = new PostController();
