/*
 * Copyright © 2018 Knative Authors (knative-dev@googlegroups.com)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package dev.knative.eventing.kafka.broker.dispatcher.impl.consumer;

import io.cloudevents.CloudEvent;
import io.cloudevents.core.v1.CloudEventBuilder;
import org.apache.kafka.clients.consumer.ConsumerInterceptor;
import org.apache.kafka.clients.consumer.ConsumerRecord;
import org.apache.kafka.clients.consumer.ConsumerRecords;
import org.apache.kafka.clients.consumer.OffsetAndMetadata;
import org.apache.kafka.common.TopicPartition;

import java.net.URI;
import java.time.Instant;
import java.time.OffsetDateTime;
import java.time.ZoneId;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.stream.Collector;
import java.util.stream.Stream;

/**
 * The {@link InvalidCloudEventInterceptor} is a {@link ConsumerInterceptor}.
 * <p>
 * Consumers might need to deal with invalid {@link ConsumerRecord}s to make progress that don't contain
 * valid {@link CloudEvent}s.
 * <p>
 * For this reason this interceptor is capable of creating a CloudEvent from {@link ConsumerRecord} metadata and data.
 */
public class InvalidCloudEventInterceptor implements ConsumerInterceptor<String, CloudEvent> {

  public static final String KIND_PLURAL_CONFIG = "interceptor.cloudevent.invalid.kind.plural";
  public static final String SOURCE_NAMESPACE_CONFIG = "interceptor.cloudevent.invalid.source.namespace";
  public static final String SOURCE_NAME_CONFIG = "interceptor.cloudevent.invalid.source.name";

  private String kindPlural;
  private String sourceNamespace;
  private String sourceName;

  @Override
  public void configure(final Map<String, ?> configs) {
    this.kindPlural = getOrDefault(configs, KIND_PLURAL_CONFIG);
    this.sourceName = getOrDefault(configs, SOURCE_NAME_CONFIG);
    this.sourceNamespace = getOrDefault(configs, SOURCE_NAMESPACE_CONFIG);
  }

  @Override
  public ConsumerRecords<String, CloudEvent> onConsume(final ConsumerRecords<String, CloudEvent> records) {
    if (records == null || records.isEmpty()) {
      return records;
    }

    final Map<TopicPartition, List<ConsumerRecord<String, CloudEvent>>> validRecords = new HashMap<>();
    for (final var record : records) {
      final var tp = new TopicPartition(record.topic(), record.partition());
      if (!validRecords.containsKey(tp)) {
        validRecords.put(tp, new ArrayList<>());
      }
      validRecords.get(tp).add(validRecord(record));
    }
    return new ConsumerRecords<>(validRecords);
  }

  @Override
  public void onCommit(Map<TopicPartition, OffsetAndMetadata> offsets) {
    // Intentionally left blank.
  }

  @Override
  public void close() {
    // Intentionally left blank.
  }

  private static String getOrDefault(final Map<String, ?> configs, final String key) {
    if (configs.containsKey(key)) {
      return (String) configs.get(key);
    }
    return "unknown";
  }

  private ConsumerRecord<String, CloudEvent> validRecord(final ConsumerRecord<String, CloudEvent> record) {
    if (!(record.value() instanceof final InvalidCloudEvent invalidEvent)) {
      return record; // Valid CloudEvent
    }
    // Handle invalid CloudEvents.
    // Create CloudEvent from the record metadata (topic, partition, offset, etc) and use received data as data field.
    // The record to CloudEvent conversion is compatible with eventing-kafka.
    final var value = new CloudEventBuilder()
      .withId(id(record))
      .withTime(time(record))
      .withType(type())
      .withSource(source(record))
      .withSubject(subject(record));

    if (invalidEvent.data() != null) {
      value.withData(invalidEvent.data());
    }

    // Put headers as event extensions.
    record.headers().forEach(header -> value.withExtension(
      replaceBadChars(header.key()),
      header.value()
    ));

    // Copy consumer record and set value to a valid CloudEvent.
    return new ConsumerRecord<>(
      record.topic(),
      record.partition(),
      record.offset(),
      record.timestamp(),
      record.timestampType(),
      record.checksum(),
      ConsumerRecord.NULL_SIZE,
      ConsumerRecord.NULL_SIZE,
      record.key(),
      value.build(),
      record.headers(),
      record.leaderEpoch()
    );
  }

  private static String replaceBadChars(String value) {
    // Since we're doing casting, the final collector is valid for
    // any possible result type, assign the result of the pipeline
    // to a typed variable, so that we're sure that our variable is
    // actually a Stream of Characters.
    final Stream<Character> validChars = value.chars()
      // The CE spec allows only lower case english letters,
      // therefore transform to lower case so that we can preserve
      // as much characters as possible.
      .mapToObj(c -> Character.toLowerCase((char) c))
      .filter(InvalidCloudEventInterceptor::allowedChar);

    return validChars
      .collect(Collector.of(
        StringBuilder::new,
        StringBuilder::append,
        StringBuilder::append,
        StringBuilder::toString
      ));
  }

  private static boolean allowedChar(final char c) {
    return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9');
  }

  private static String subject(final ConsumerRecord<String, CloudEvent> record) {
    return "partition:" + record.partition() + "#" + record.offset();
  }

  private URI source(final ConsumerRecord<String, CloudEvent> record) {
    return URI.create(
      "/apis/v1/namespaces/" +
        this.sourceNamespace +
        "/" +
        this.kindPlural +
        "/" +
        this.sourceName +
        "#" +
        record.topic()
    );
  }

  private static String type() {
    return "dev.knative.kafka.event";
  }

  private static OffsetDateTime time(final ConsumerRecord<String, CloudEvent> record) {
    return OffsetDateTime.ofInstant(Instant.ofEpochMilli(record.timestamp()), ZoneId.of("UTC"));
  }

  private static String id(final ConsumerRecord<String, CloudEvent> record) {
    return "partition:" + record.partition() + "/offset:" + record.offset();
  }
}
