// Thin wrappers around the Wails runtime + bound backend methods.
// Using direct imports from generated Wails code instead of window globals
// to avoid initialization timing issues.
import * as App from '../../wailsjs/go/backend/App.js'
import * as Runtime from '../../wailsjs/runtime/runtime.js'

export const Backend = App

export const Events = {
  on: (event, handler) => Runtime.EventsOn?.(event, handler),
  off: (event) => Runtime.EventsOff?.(event),
  once: (event, handler) => Runtime.EventsOnce?.(event, handler),
  emit: (event, ...data) => Runtime.EventsEmit?.(event, ...data),
}
