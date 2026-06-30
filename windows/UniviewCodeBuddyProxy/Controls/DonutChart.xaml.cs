using System;
using System.Collections.Generic;
using Microsoft.UI;
using Windows.Foundation;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Path = Microsoft.UI.Xaml.Shapes.Path;
using UniviewCodeBuddyProxy.Helpers;

namespace UniviewCodeBuddyProxy.Controls;

/// <summary>
/// A data segment for the donut chart.
/// </summary>
public sealed class DonutSegment
{
    public string Name { get; set; } = "";
    public double Ratio { get; set; }
    public Color Color { get; set; }
}

/// <summary>
/// Reusable donut/ring chart control with center text and segment drawing.
/// Extracted from DashboardPage inline Canvas drawing.
/// Mirrors macOS DonutChart.swift.
/// </summary>
public sealed partial class DonutChart : UserControl
{
    public DonutChart()
    {
        this.InitializeComponent();
        Loaded += OnLoaded;
    }

    // ── Dependency Properties ──

    public static readonly DependencyProperty SegmentsProperty =
        DependencyProperty.Register(nameof(Segments), typeof(IList<DonutSegment>), typeof(DonutChart),
            new PropertyMetadata(null, OnSegmentsChanged));

    public IList<DonutSegment> Segments
    {
        get => (IList<DonutSegment>)GetValue(SegmentsProperty);
        set => SetValue(SegmentsProperty, value);
    }

    public static readonly DependencyProperty CenterTextProperty =
        DependencyProperty.Register(nameof(CenterText), typeof(string), typeof(DonutChart),
            new PropertyMetadata(""));

    public string CenterText
    {
        get => (string)GetValue(CenterTextProperty);
        set => SetValue(CenterTextProperty, value);
    }

    public static readonly DependencyProperty SubLabelTextProperty =
        DependencyProperty.Register(nameof(SubLabelText), typeof(string), typeof(DonutChart),
            new PropertyMetadata(""));

    public string SubLabelText
    {
        get => (string)GetValue(SubLabelTextProperty);
        set => SetValue(SubLabelTextProperty, value);
    }

    private static void OnSegmentsChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        ((DonutChart)d).DrawSegments();
    }

    private void OnLoaded(object sender, RoutedEventArgs e) => DrawSegments();

    private void DrawSegments()
    {
        var canvas = DonutCanvas;
        canvas.Children.Clear();

        if (Segments == null || Segments.Count == 0) return;

        double centerX = 65, centerY = 65, radius = 42, strokeThickness = 16;
        double startAngle = -90; // Start from top

        foreach (var segment in Segments)
        {
            double sweepAngle = segment.Ratio * 360;

            var path = new Path
            {
                Stroke = new SolidColorBrush(segment.Color),
                StrokeThickness = strokeThickness,
                StrokeStartLineCap = PenLineCap.Round,
                StrokeEndLineCap = PenLineCap.Round,
                Data = CreateArcGeometry(centerX, centerY, radius, startAngle, sweepAngle)
            };

            canvas.Children.Add(path);
            startAngle += sweepAngle;
        }
    }

    private static Geometry CreateArcGeometry(double cx, double cy, double r, double startAngle, double sweepAngle)
    {
        double startRad = startAngle * Math.PI / 180;
        double endRad = (startAngle + sweepAngle) * Math.PI / 180;

        double x1 = cx + r * Math.Cos(startRad);
        double y1 = cy + r * Math.Sin(startRad);
        double x2 = cx + r * Math.Cos(endRad);
        double y2 = cy + r * Math.Sin(endRad);

        bool isLargeArc = sweepAngle > 180;

        var figure = new PathFigure { StartPoint = new Point(x1, y1) };
        figure.Segments.Add(new ArcSegment
        {
            Point = new Point(x2, y2),
            Size = new Size(r, r),
            IsLargeArc = isLargeArc,
            SweepDirection = SweepDirection.Clockwise
        });

        var geometry = new PathGeometry();
        geometry.Figures.Add(figure);
        return geometry;
    }
}
