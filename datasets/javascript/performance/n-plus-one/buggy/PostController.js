const { Post, Author } = require('../models');

class PostController {
  async getAllPosts(req, res) {
    try {
      const posts = await Post.findAll({ where: { published: true } });
      
      // BUG: N+1 Query Problem
      // Fetching author for each post individually in a loop
      const postsWithAuthors = [];
      for (const post of posts) {
        const author = await Author.findById(post.authorId);
        postsWithAuthors.push({
          ...post.toJSON(),
          author: author.name
        });
      }

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
