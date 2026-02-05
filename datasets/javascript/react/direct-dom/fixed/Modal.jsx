import React from 'react';
import ReactDOM from 'react-dom';

const Modal = ({ isOpen, children }) => {
  if (!isOpen) return null;

  // FIXED: Use React Portals for rendering outside the parent hierarchy
  return ReactDOM.createPortal(
    <div className="modal-overlay">
      <div className="modal-content">
        {children}
      </div>
    </div>,
    document.body // Or a specific root element
  );
};

export default Modal;
