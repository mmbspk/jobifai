import '@testing-library/jest-dom'

// jsdom does not implement scrollIntoView — stub it globally
globalThis.HTMLElement.prototype.scrollIntoView = () => {}
