// Package openrouter is a client for the OpenRouter.ai API.
//
// It is standard library only. Every decision in this package is made in the
// service of one property: a stream that was cut short must not be presented as
// a complete reply, and text that arrived before a failure must not be thrown
// away with the failure.
//
// The client posts to /chat/completions with a bearer credential and reads the
// reply as Server-Sent Events. It requires the terminating [DONE] marker rather
// than assuming it. A stream that ends without that marker was cut, and
// [Chat] reports it as such through the event callback while keeping the text
// that arrived.
//
// Failures are reported through the callback, not as a return value. The partial
// text is usually more useful than an error alone, and a caller that receives
// only an error has nothing to show the reader.
//
// Tool call fragments are reassembled here, in the transport, because this is the
// one place that knows how the pieces were framed. A caller reassembling is a
// caller that can get it wrong. Fragments are joined on the wire index rather
// than arrival order, since the index is the only field every fragment carries.
// A call whose name never arrived is dropped, because there would be nothing to
// run.
//
// Usage and cost are pointers throughout. A reported zero and an absent value
// are different, and a plain integer cannot tell them apart.
//
// This package never runs a request on behalf of a model. It does not know that
// tools exist. The interface package that draws a tool call is what runs it.
package openrouter
