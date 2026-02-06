import React from 'react';
import PropTypes from 'prop-types';

const CommentView = ({ comment, author }) => {
  if (!comment) return null;

  return (
    <div className="comment-container">
      <div className="comment-header">
        <span className="author-name">{author.name}</span>
        <span className="timestamp">{new Date(comment.createdAt).toLocaleDateString()}</span>
      </div>
      
      {/* BUG: Vulnerable to XSS */}
      <div 
        className="comment-content"
        dangerouslySetInnerHTML={{ __html: comment.content }}
      />
      
      <div className="comment-actions">
        <button onClick={() => console.log('Upvoted')}>Upvote</button>
        <button onClick={() => console.log('Reply')}>Reply</button>
      </div>
    </div>
  );
};

CommentView.propTypes = {
  comment: PropTypes.shape({
    id: PropTypes.string.isRequired,
    content: PropTypes.string.isRequired,
    createdAt: PropTypes.string.isRequired
  }),
  author: PropTypes.shape({
    name: PropTypes.string.isRequired,
    avatar: PropTypes.string
  })
};

export default CommentView;
