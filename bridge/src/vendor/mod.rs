// SPDX-License-Identifier: AGPL-3.0-only

//! Vendor protocols: devices that send their richest data on a service of
//! their maker's rather than on a Bluetooth SIG profile (a Polar strap's raw
//! ECG, for one). Each driver is a state machine without I/O: it says what to
//! subscribe to, read and write, and is fed what the device answers. `ble`
//! runs it over btleplug on a computer; a phone's Bluetooth stack can run the
//! same driver, since nothing in it touches a radio, a clock or a file.
//!
//! Values still go up as the bytes the device sent. A driver adds `ctx`, the
//! bytes that tell the server how to read a value (for Polar, the settings a
//! stream was started with), so the server decodes each line on its own.

pub mod polar;

use uuid::Uuid;

/// What a driver asks the radio to do, in order.
#[derive(Clone, Debug, PartialEq)]
pub enum Action {
    /// Turn on notifications or indications.
    Subscribe(Uuid),
    /// Read the value; it comes back through `Driver::input` with `read` set.
    Read(Uuid),
    /// Write the bytes, with response.
    Write(Uuid, Vec<u8>),
}

/// What the live view shows for a value.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct Live {
    pub text: String,
    /// A heart rate the value carries, for the status line.
    pub heart_rate: Option<u16>,
}

/// What a driver answers to an input.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct Step {
    pub actions: Vec<Action>,
    /// Upload the value, with these context bytes (empty for none).
    pub upload: Option<Vec<u8>>,
    pub live: Option<Live>,
    /// Something the person should know: a stream the device refused, a
    /// request it never answered. Never a health value.
    pub note: Option<String>,
}

impl Step {
    fn act(actions: Vec<Action>) -> Step {
        Step {
            actions,
            ..Default::default()
        }
    }

    fn note(mut self, text: impl Into<String>) -> Step {
        self.note = Some(text.into());
        self
    }
}

/// A vendor protocol for one connection.
pub trait Driver: Send {
    /// The first actions, once the services are discovered.
    fn start(&mut self) -> Vec<Action>;
    /// Whether the values of this characteristic are the driver's.
    fn owns(&self, char: &Uuid) -> bool;
    /// A value of one of its characteristics: read (`read`), or notified.
    fn input(&mut self, char: &Uuid, value: &[u8], read: bool) -> Step;
    /// Whether an answer is awaited: the radio calls `timeout` when it does
    /// not come.
    fn waiting(&self) -> bool;
    fn timeout(&mut self) -> Step;
}

/// Which vendor protocols to run, and what to ask of them.
#[derive(Clone, Debug, PartialEq)]
pub struct Config {
    /// Polar measurement types to stream; empty turns the Polar driver off.
    pub polar: Vec<polar::Kind>,
}

impl Default for Config {
    fn default() -> Config {
        Config {
            polar: polar::DEFAULT_STREAMS.to_vec(),
        }
    }
}

/// The driver for a device with these characteristics, if one applies.
pub fn detect(chars: &[Uuid], cfg: &Config) -> Option<Box<dyn Driver>> {
    if !cfg.polar.is_empty() && polar::present(chars) {
        return Some(Box::new(polar::Polar::new(&cfg.polar)));
    }
    None
}
