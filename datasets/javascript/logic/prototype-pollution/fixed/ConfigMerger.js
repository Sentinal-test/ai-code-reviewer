class ConfigMerger {
  constructor(defaultConfig) {
    this.config = defaultConfig || {};
  }

  merge(payload) {
    this.mergeRecursive(this.config, payload);
    return this.config;
  }

  mergeRecursive(target, source) {
    for (const key in source) {
      if (key === '__proto__' || key === 'constructor' || key === 'prototype') {
        continue;
      }
      
      if (typeof source[key] === 'object' && source[key] !== null) {
        if (!target[key]) target[key] = {};
        this.mergeRecursive(target[key], source[key]);
      } else {
        target[key] = source[key];
      }
    }
    return target;
  }
}

module.exports = ConfigMerger;
